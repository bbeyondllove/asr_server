package session

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"asr_server/config"
	"asr_server/internal/logger"
	"asr_server/internal/pool"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
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
	SendQueue    chan interface{}
	sendDone     chan struct{}
	sendErrCount int32

	// 活跃性检测
	lastActivity time.Time
}

// Manager 会话管理器
type Manager struct {
	sessions   map[string]*Session
	recognizer *sherpa.OfflineRecognizer
	vadPool    *pool.VADPool
	mu         sync.RWMutex

	// 统计信息
	totalSessions  int64
	activeSessions int64
	totalMessages  int64

	// 清理
	ctx    context.Context
	cancel context.CancelFunc
}

// 全局缓冲区池（8KB）
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 8192)
	},
}

// 全局float32切片池（最大支持8KB/2=4096采样点）
var float32Pool = sync.Pool{}

func getFloat32PoolSlice() []float32 {
	chunkSize := config.GlobalConfig.Audio.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 4096
	}
	return make([]float32, chunkSize)
}

// NewManager 创建新的会话管理器
func NewManager(recognizer *sherpa.OfflineRecognizer, vadPool *pool.VADPool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		sessions:   make(map[string]*Session),
		recognizer: recognizer,
		vadPool:    vadPool,
		ctx:        ctx,
		cancel:     cancel,
	}

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
		ID:           sessionID,
		Conn:         conn,
		VADInstance:  vadInstance,
		LastSeen:     time.Now().UnixNano(),
		closed:       0,
		SendQueue:    make(chan interface{}, config.GlobalConfig.Session.SendQueueSize),
		sendDone:     make(chan struct{}),
		sendErrCount: 0,
		lastActivity: time.Now(),
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
		case msg := <-s.SendQueue:
			if atomic.LoadInt32(&s.closed) == 1 {
				return
			}

			// 直接写消息，不再设置写超时
			if err := s.Conn.WriteJSON(msg); err != nil {
				atomic.AddInt32(&s.sendErrCount, 1)
				logger.Errorf("Failed to send message to session %s: %v", s.ID, err)
				// 如果连续错误超过阈值，关闭会话
				if atomic.LoadInt32(&s.sendErrCount) > int32(config.GlobalConfig.Session.MaxSendErrors) {
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

	// 更新会话活跃时间
	atomic.StoreInt64(&session.LastSeen, time.Now().UnixNano())
	atomic.AddInt64(&m.totalMessages, 1)

	// 验证输入数据
	if len(audioData) == 0 {
		logger.Warnf("Session %s: Received empty audio data", sessionID)
		return fmt.Errorf("empty audio data")
	}

	if len(audioData)%2 != 0 {
		logger.Warnf("Session %s: Audio data length %d is not even (expecting 16-bit samples)", sessionID, len(audioData))
		return fmt.Errorf("invalid audio data length: %d", len(audioData))
	}

	// 在ProcessAudioData中复用float32切片
	numSamples := len(audioData) / 2
	samples := float32Pool.Get()
	var float32Slice []float32
	if samples == nil {
		float32Slice = getFloat32PoolSlice()
	} else {
		float32Slice = samples.([]float32)
	}
	if cap(float32Slice) < numSamples {
		float32Slice = make([]float32, numSamples)
	}
	float32Slice = float32Slice[:numSamples]
	defer float32Pool.Put(float32Slice)
	normalizeFactor := config.GlobalConfig.Audio.NormalizeFactor
	for i := 0; i < numSamples; i++ {
		sample := int16(audioData[i*2]) | int16(audioData[i*2+1])<<8
		float32Slice[i] = float32(sample) / normalizeFactor
	}

	logger.Debugf("Session %s: Converted %d bytes to %d float32 samples", sessionID, len(audioData), numSamples)

	// VAD检测 - 使用响应超时配置
	vadTimeout := time.Duration(config.GlobalConfig.Response.Timeout) * time.Second
	vadCtx, vadCancel := context.WithTimeout(context.Background(), vadTimeout)
	defer vadCancel()

	// 在goroutine中执行VAD处理，避免阻塞
	vadDone := make(chan struct{})
	go func() {
		defer close(vadDone)
		session.VADInstance.VAD.AcceptWaveform(float32Slice)
	}()

	// 等待VAD处理完成或超时
	select {
	case <-vadDone:
		// VAD处理完成
	case <-vadCtx.Done():
		logger.Warnf("Session %s: VAD processing timeout", sessionID)
		return fmt.Errorf("VAD processing timeout")
	}

	// 处理语音段 - 直接使用VAD检测的原始语
	segmentCount := 0
	var speechSegments [][]float32
	sampleRate := config.GlobalConfig.Audio.SampleRate

	// 收集所有有效的语音段
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
			duration := float64(len(segment.Samples)) / float64(sampleRate)
			minSpeechDuration := float64(config.GlobalConfig.VAD.MinSpeechDuration)
			if duration < minSpeechDuration {
				logger.Debugf("Session %s: Skipping short segment %d (%.2fs < %.2fs)", sessionID, segmentCount, duration, minSpeechDuration)
				continue
			}

			// 检查最大时长
			maxDuration := float64(config.GlobalConfig.VAD.MaxSpeechDuration)
			if duration > maxDuration {
				logger.Warnf("Session %s: Segment %d too long (%.2fs > %.2fs), truncating", sessionID, segmentCount, duration, maxDuration)
				maxSamples := int(maxDuration * float64(sampleRate))
				segment.Samples = segment.Samples[:maxSamples]
			}

			speechSegments = append(speechSegments, segment.Samples)
			logger.Debugf("Session %s: Collected segment %d with %d samples (%.2fs)", sessionID, segmentCount, len(segment.Samples), duration)
		} else {
			logger.Warnf("Session %s: Empty or null speech segment %d", sessionID, segmentCount)
		}
	}

	// 处理收集到的语音段
	for i, samples := range speechSegments {
		// 提交识别任务
		taskID := fmt.Sprintf("%s_%d_%d", sessionID, time.Now().UnixNano(), i)
		go func(samples []float32, sampleRate int, sessionID string, taskID string) {
			stream := sherpa.NewOfflineStream(m.recognizer)
			defer sherpa.DeleteOfflineStream(stream)
			stream.AcceptWaveform(sampleRate, samples)
			m.recognizer.Decode(stream)
			result := stream.GetResult()
			if result != nil {
				m.handleRecognitionResult(sessionID, result.Text, nil)
			} else {
				m.handleRecognitionResult(sessionID, "", fmt.Errorf("recognition failed"))
			}
		}(samples, sampleRate, sessionID, taskID)
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
		case session.SendQueue <- errorMsg:
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
	case session.SendQueue <- response:
		logger.Infof("Recognition result queued for session %s: %s", sessionID, result)
	default:
		logger.Warnf("Session %s send queue is full, dropping recognition result", sessionID)
	}
}

// closeSession 关闭会话
func (m *Manager) closeSession(session *Session) {
	if atomic.CompareAndSwapInt32(&session.closed, 0, 1) {
		// 关闭发送通道
		close(session.sendDone)
		// 清空发送队列
		for len(session.SendQueue) > 0 {
			<-session.SendQueue
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

// GetStats 获取管理器统计信息 - 增强版本
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 获取资源池统计
	poolStats := m.vadPool.GetStats()

	return map[string]interface{}{
		"total_sessions":   atomic.LoadInt64(&m.totalSessions),
		"active_sessions":  atomic.LoadInt64(&m.activeSessions),
		"total_messages":   atomic.LoadInt64(&m.totalMessages),
		"current_sessions": len(m.sessions),
		"pool_stats":       poolStats,
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
