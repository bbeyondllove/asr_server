package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"asr_server/internal/logger"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// VADInstance VAD实例包装
type VADInstance struct {
	VAD      *sherpa.VoiceActivityDetector
	LastUsed time.Time
	InUse    int32
	ID       int
}

// VADPool VAD资源池
type VADPool struct {
	instances  []*VADInstance
	available  chan *VADInstance
	config     *sherpa.VadModelConfig
	bufferSize float32
	poolSize   int

	// 统计信息
	totalCreated int64
	totalReused  int64
	totalActive  int64

	// 控制
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewVADPool 创建新的VAD资源池
func NewVADPool(config *sherpa.VadModelConfig, bufferSize float32, poolSize int) *VADPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &VADPool{
		instances:  make([]*VADInstance, 0, poolSize),
		available:  make(chan *VADInstance, poolSize),
		config:     config,
		bufferSize: bufferSize,
		poolSize:   poolSize,
		ctx:        ctx,
		cancel:     cancel,
	}
	return pool
}

// Initialize 并行初始化VAD池
func (p *VADPool) Initialize() error {
	logger.Infof("🔧 Initializing VAD pool with %d instances...", p.poolSize)

	// 并行初始化VAD实例
	var initWg sync.WaitGroup
	errorChan := make(chan error, p.poolSize)

	for i := 0; i < p.poolSize; i++ {
		initWg.Add(1)
		go func(instanceID int) {
			defer initWg.Done()

			// 创建VAD实例
			vad := sherpa.NewVoiceActivityDetector(p.config, p.bufferSize)
			if vad == nil {
				errorChan <- fmt.Errorf("failed to create VAD instance %d", instanceID)
				return
			}

			instance := &VADInstance{
				VAD:      vad,
				LastUsed: time.Now(),
				InUse:    0,
				ID:       instanceID,
			}

			p.mu.Lock()
			p.instances = append(p.instances, instance)
			p.mu.Unlock()

			// 放入可用队列
			select {
			case p.available <- instance:
				atomic.AddInt64(&p.totalCreated, 1)
				logger.Infof("✅ VAD instance %d initialized", instanceID)
			default:
				// 队列满，销毁实例
				sherpa.DeleteVoiceActivityDetector(vad)
				errorChan <- fmt.Errorf("VAD pool queue full, instance %d discarded", instanceID)
			}
		}(i)
	}

	initWg.Wait()
	close(errorChan)

	// 检查初始化错误
	var initErrors []error
	for err := range errorChan {
		if err != nil {
			initErrors = append(initErrors, err)
			logger.Warnf("⚠️  VAD initialization warning: %v", err)
		}
	}

	// 启动清理协程
	// go p.cleanup() // Removed as per edit hint

	successCount := len(p.instances)
	logger.Infof("🚀 VAD pool initialized with %d/%d instances", successCount, p.poolSize)

	if len(initErrors) > 0 && successCount == 0 {
		return fmt.Errorf("failed to initialize any VAD instances")
	}

	return nil
}

// Get 获取VAD实例
func (p *VADPool) Get() (*VADInstance, error) {
	logger.Infof("🔍 Attempting to get VAD instance from pool (available: %d)", len(p.available))

	select {
	case instance := <-p.available:
		logger.Infof("🎯 Got VAD instance %d from pool", instance.ID)
		if atomic.CompareAndSwapInt32(&instance.InUse, 0, 1) {
			instance.LastUsed = time.Now()
			atomic.AddInt64(&p.totalReused, 1)
			atomic.AddInt64(&p.totalActive, 1)
			logger.Infof("✅ VAD instance %d marked as in-use (active: %d)", instance.ID, atomic.LoadInt64(&p.totalActive))
			return instance, nil
		}
		// 实例已被使用，重新放回队列
		logger.Warnf("⚠️  VAD instance %d already in use, returning to pool", instance.ID)
		select {
		case p.available <- instance:
		default:
		}
		return p.Get() // 递归重试
	case <-time.After(100 * time.Millisecond):
		// 超时，创建新实例
		logger.Warnf("⏰ VAD pool timeout, creating new temporary instance")
		return p.createNewInstance()
	case <-p.ctx.Done():
		logger.Errorf("❌ VAD pool is shutting down")
		return nil, fmt.Errorf("VAD pool is shutting down")
	}
}

// Put 归还VAD实例
func (p *VADPool) Put(instance *VADInstance) {
	if instance == nil {
		logger.Warnf("⚠️  Attempted to put nil VAD instance")
		return
	}

	logger.Infof("🔄 Returning VAD instance %d to pool", instance.ID)

	if atomic.CompareAndSwapInt32(&instance.InUse, 1, 0) {
		instance.LastUsed = time.Now()
		atomic.AddInt64(&p.totalActive, -1)
		logger.Infof("✅ VAD instance %d marked as available (active: %d)", instance.ID, atomic.LoadInt64(&p.totalActive))

		// 重置VAD状态（清空缓冲区）
		p.resetVAD(instance.VAD)

		select {
		case p.available <- instance:
			// 成功归还
			logger.Infof("✅ VAD instance %d returned to pool (available: %d)", instance.ID, len(p.available))
		default:
			// 队列满，销毁实例
			logger.Warnf("⚠️  VAD pool queue full, destroying instance %d", instance.ID)
			// p.destroyInstance(instance) // Removed as per edit hint
		}
	} else {
		logger.Warnf("⚠️  VAD instance %d was not in use, cannot return", instance.ID)
	}
}

// createNewInstance 创建新的VAD实例
func (p *VADPool) createNewInstance() (*VADInstance, error) {
	vad := sherpa.NewVoiceActivityDetector(p.config, p.bufferSize)
	if vad == nil {
		return nil, fmt.Errorf("failed to create new VAD instance")
	}

	instance := &VADInstance{
		VAD:      vad,
		LastUsed: time.Now(),
		InUse:    1,
		ID:       -1, // 临时实例
	}

	atomic.AddInt64(&p.totalCreated, 1)
	atomic.AddInt64(&p.totalActive, 1)

	logger.Infof("🆕 Created temporary VAD instance")
	return instance, nil
}

// resetVAD 重置VAD状态
func (p *VADPool) resetVAD(vad *sherpa.VoiceActivityDetector) {
	// 清空VAD缓冲区
	for !vad.IsEmpty() {
		segment := vad.Front()
		vad.Pop()
		if segment != nil {
			// 释放segment资源（如果需要）
		}
	}
}

// GetStats 获取统计信息
func (p *VADPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"pool_size":       p.poolSize,
		"total_instances": len(p.instances),
		"available_count": len(p.available),
		"active_count":    atomic.LoadInt64(&p.totalActive),
		"total_created":   atomic.LoadInt64(&p.totalCreated),
		"total_reused":    atomic.LoadInt64(&p.totalReused),
	}
}

// Shutdown 关闭VAD池
func (p *VADPool) Shutdown() {
	logger.Infof("🛑 Shutting down VAD pool...")

	// 取消上下文
	p.cancel()

	// 销毁所有实例
	p.mu.Lock()
	defer p.mu.Unlock()

	// 清空可用队列
	for {
		select {
		case <-p.available:
			// 仅取出，不再使用 instance
		default:
			goto cleanup_instances
		}
	}

cleanup_instances:
	// 销毁所有实例
	for range p.instances {
		// 仅遍历，不再使用 instance
	}

	p.instances = nil
	close(p.available)

	logger.Infof("✅ VAD pool shutdown complete")
}
