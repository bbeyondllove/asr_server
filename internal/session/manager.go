package session

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"asr_server/internal/logger"
	"asr_server/internal/pool"
)

// Session WebSocket会话
type Session struct {
	ID          string
	Conn        *websocket.Conn
	VADInstance *pool.VADInstance
	LastSeen    int64 // 使用int64存储时间戳
	mu          sync.RWMutex
	closed      int32

	// 发送队列和通道
	sendQueue    chan interface{}
	sendDone     chan struct{}
	sendErrCount int32
}

// Manager 会话管理器
type Manager struct {
	sessions map[string]*Session
	pool     interface {
		SubmitTask(task *pool.Task) error
		GetStats() map[string]interface{}
	}
	vadPool *pool.VADPool
	mu      sync.RWMutex

	// 统计信息
	totalSessions  int64
	activeSessions int64
	totalMessages  int64

	// 清理
	cleanupTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewManager 创建新的会话管理器
func NewManager(resourcePool interface {
	SubmitTask(task *pool.Task) error
	GetStats() map[string]interface{}
}, vadPool *pool.VADPool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		sessions:      make(map[string]*Session),
		pool:          resourcePool,
		vadPool:       vadPool,
		ctx:           ctx,
		cancel:        cancel,
		cleanupTicker: time.NewTicker(30 * time.Second),
	}

	// 启动清理协程
	go manager.cleanup()

	return manager
}

// CreateSession 创建新会话
func (m *Manager) CreateSession(sessionID string, conn *websocket.Conn) (*Session, error) {
	// 从VAD池获取实例
	vadInstance, err := m.vadPool.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get VAD instance for session %s: %v", sessionID, err)
	}

	session := &Session{
		ID:          sessionID,
		Conn:        conn,
		VADInstance: vadInstance,
		LastSeen:    time.Now().UnixNano(),
		sendQueue:   make(chan interface{}, 100), // 默认队列大小100
		sendDone:    make(chan struct{}),
	}

	// 启动发送协程
	go session.sendLoop()

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	atomic.AddInt64(&m.totalSessions, 1)
	atomic.AddInt64(&m.activeSessions, 1)

	logger.Infof("✅ Session %s created with VAD instance %d", sessionID, vadInstance.ID)
	return session, nil
}

// GetSession 获取会话
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.RLock()
	session, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if exists {
		// 使用原子操作更新LastSeen
		atomic.StoreInt64(&session.LastSeen, time.Now().UnixNano())
	}

	return session, exists
}

// RemoveSession 移除会话
func (m *Manager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.sessions[sessionID]; exists {
		m.closeSession(session)
		delete(m.sessions, sessionID)
		atomic.AddInt64(&m.activeSessions, -1)
		logger.Infof("🗑️  Session %s removed", sessionID)
	}
}

// sendLoop 发送循环
func (s *Session) sendLoop() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("❌ Send loop panicked for session %s: %v", s.ID, r)
		}
	}()

	for {
		select {
		case msg := <-s.sendQueue:
			if atomic.LoadInt32(&s.closed) == 1 {
				return
			}

			if err := s.Conn.WriteJSON(msg); err != nil {
				atomic.AddInt32(&s.sendErrCount, 1)
				logger.Errorf("Failed to send message to session %s: %v", s.ID, err)
				// 如果连续错误超过阈值，关闭会话
				if atomic.LoadInt32(&s.sendErrCount) > 5 {
					logger.Errorf("Too many send errors for session %s, closing", s.ID)
					atomic.StoreInt32(&s.closed, 1)
					return
				}
			} else {
				atomic.StoreInt32(&s.sendErrCount, 0)
			}
		case <-s.sendDone:
			return
		}
	}
}

// closeSession 关闭会话
func (m *Manager) closeSession(session *Session) {
	if atomic.CompareAndSwapInt32(&session.closed, 0, 1) {
		// 关闭发送通道
		close(session.sendDone)
		// 清空发送队列
		for len(session.sendQueue) > 0 {
			<-session.sendQueue
		}

		if session.VADInstance != nil {
			m.vadPool.Put(session.VADInstance)
			session.VADInstance = nil
		}
		if session.Conn != nil {
			session.Conn.Close()
		}
	}
}

// ProcessAudioData 处理音频数据 - 增强版本
func (m *Manager) ProcessAudioData(sessionID string, audioData []byte) error {
	session, exists := m.GetSession(sessionID)
	if !exists {
		logger.Errorf("Session %s not found when processing audio data", sessionID)
		return fmt.Errorf("session %s not found", sessionID)
	}

	if atomic.LoadInt32(&session.closed) == 1 {
		logger.Errorf("Session %s is closed, cannot process audio data", sessionID)
		return fmt.Errorf("session %s is closed", sessionID)
	}

	atomic.AddInt64(&m.totalMessages, 1)

	// 检查资源池队列状态
	queueStats := m.pool.GetStats()
	if queueLength, ok := queueStats["queue_length"].(int64); ok {
		if queueLength > 400 { // 队列长度超过80%时警告
			logger.Warnf("Session %s: ASR queue is busy (length: %d), processing may be delayed", sessionID, queueLength)
		}
	}

	// 验证输入数据
	if len(audioData) == 0 {
		logger.Warnf("Session %s: Received empty audio data", sessionID)
		return fmt.Errorf("empty audio data")
	}

	if len(audioData)%2 != 0 {
		logger.Warnf("Session %s: Audio data length %d is not even (expecting 16-bit samples)", sessionID, len(audioData))
		return fmt.Errorf("invalid audio data length: %d", len(audioData))
	}

	// 转换音频数据为float32
	numSamples := len(audioData) / 2
	samples := make([]float32, numSamples)
	for i := 0; i < numSamples; i++ {
		// 小端序读取16位样本
		sample := int16(audioData[i*2]) | int16(audioData[i*2+1])<<8
		// 归一化到 [-1, 1] 范围
		samples[i] = float32(sample) / 32768.0
	}

	logger.Debugf("Session %s: Converted %d bytes to %d float32 samples", sessionID, len(audioData), numSamples)

	// VAD检测
	session.VADInstance.VAD.AcceptWaveform(samples)

	// 处理语音段
	segmentCount := 0
	for !session.VADInstance.VAD.IsEmpty() {
		segment := session.VADInstance.VAD.Front()
		session.VADInstance.VAD.Pop()
		segmentCount++

		if segment != nil && len(segment.Samples) > 0 {
			// 再次检查会话状态
			if atomic.LoadInt32(&session.closed) == 1 {
				logger.Warnf("Session %s closed during speech segment processing", sessionID)
				return fmt.Errorf("session %s closed during processing", sessionID)
			}

			// 验证音频数据
			if len(segment.Samples) == 0 {
				logger.Warnf("Session %s: Speech segment %d has no samples", sessionID, segmentCount)
				continue
			}

			// 音频时长检查
			duration := float64(len(segment.Samples)) / 16000.0 // 假设16kHz采样率
			if duration < 0.1 {
				logger.Debugf("Session %s: Skipping short segment %d (%.2fs)", sessionID, segmentCount, duration)
				continue
			}

			// 提交识别任务
			taskID := fmt.Sprintf("%s_%d", sessionID, time.Now().UnixNano())
			task := &pool.Task{
				ID:         taskID,
				SessionID:  sessionID,
				Samples:    segment.Samples,
				SampleRate: 16000, // 设置采样率
				Context:    context.WithValue(m.ctx, "sessionID", sessionID),
				Callback: func(result string, err error) {
					m.handleRecognitionResult(sessionID, result, err)
				},
			}

			logger.Debugf("Session %s: Submitting task %s with %d samples (%.2fs)", sessionID, taskID, len(segment.Samples), duration)

			if err := m.pool.SubmitTask(task); err != nil {
				logger.Errorf("Failed to submit recognition task for session %s: %v", sessionID, err)
				return err
			}
		} else {
			logger.Warnf("Session %s: Empty or null speech segment %d", sessionID, segmentCount)
		}
	}

	return nil
}

// handleRecognitionResult 处理识别结果
func (m *Manager) handleRecognitionResult(sessionID, result string, err error) {
	session, exists := m.GetSession(sessionID)
	if !exists {
		logger.Warnf("Session %s not found when handling recognition result, session may have been closed", sessionID)
		return
	}

	// 检查会话是否已关闭
	if atomic.LoadInt32(&session.closed) == 1 {
		logger.Warnf("Session %s is closed when handling recognition result", sessionID)
		return
	}

	if err != nil {
		logger.Errorf("Recognition error for session %s: %v", sessionID, err)
		// 发送错误消息
		errorMsg := map[string]interface{}{
			"type":      "error",
			"message":   "Recognition failed",
			"error":     err.Error(),
			"timestamp": time.Now().UnixMilli(),
		}

		// 非阻塞发送
		select {
		case session.sendQueue <- errorMsg:
		default:
			logger.Warnf("Session %s send queue is full, dropping error message", sessionID)
		}
		return
	}

	// 发送识别结果
	response := map[string]interface{}{
		"type":      "final",
		"text":      result,
		"timestamp": time.Now().UnixMilli(),
	}

	// 非阻塞发送
	select {
	case session.sendQueue <- response:
		logger.Infof("Recognition result queued for session %s: %s", sessionID, result)
	default:
		logger.Warnf("Session %s send queue is full, dropping recognition result", sessionID)
	}
}

// cleanup 清理过期会话
func (m *Manager) cleanup() {
	defer m.cleanupTicker.Stop()

	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupExpiredSessions()
		case <-m.ctx.Done():
			return
		}
	}
}

// cleanupExpiredSessions 清理过期会话
func (m *Manager) cleanupExpiredSessions() {
	now := time.Now()
	expiredSessions := make([]string, 0)

	m.mu.RLock()
	for sessionID, session := range m.sessions {
		session.mu.RLock()
		if now.Sub(time.Unix(0, session.LastSeen)) > 5*time.Minute {
			expiredSessions = append(expiredSessions, sessionID)
		}
		session.mu.RUnlock()
	}
	m.mu.RUnlock()

	// 移除过期会话
	for _, sessionID := range expiredSessions {
		logger.Infof("🧹 Cleaning up expired session: %s", sessionID)
		m.RemoveSession(sessionID)
	}

	if len(expiredSessions) > 0 {
		logger.Infof("🧹 Cleaned up %d expired sessions", len(expiredSessions))
	}
}

// GetStats 获取管理器统计信息 - 增强版本
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 获取资源池统计
	poolStats := m.pool.GetStats()
	vadStats := m.vadPool.GetStats()

	return map[string]interface{}{
		"total_sessions":   atomic.LoadInt64(&m.totalSessions),
		"active_sessions":  atomic.LoadInt64(&m.activeSessions),
		"total_messages":   atomic.LoadInt64(&m.totalMessages),
		"current_sessions": len(m.sessions),
		"pool_stats":       poolStats,
		"vad_stats":        vadStats,
	}
}

// Shutdown 关闭管理器
func (m *Manager) Shutdown() {
	logger.Infof("🛑 Shutting down session manager...")

	// 取消上下文
	m.cancel()

	// 关闭所有会话
	m.mu.Lock()
	for sessionID, session := range m.sessions {
		logger.Infof("🛑 Closing session: %s", sessionID)
		m.closeSession(session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	logger.Infof("✅ Session manager shutdown complete")
}
