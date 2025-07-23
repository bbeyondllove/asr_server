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

// SileroVADConfig Silero VAD配置
type SileroVADConfig struct {
	ModelConfig       *sherpa.VadModelConfig
	BufferSizeSeconds float32
	PoolSize          int
	MaxIdle           int
}

// SileroVADInstance Silero VAD实例
type SileroVADInstance struct {
	ID       int
	VAD      *sherpa.VoiceActivityDetector
	LastUsed int64
	InUse    int32
	mu       sync.RWMutex
}

// GetID 获取实例ID
func (i *SileroVADInstance) GetID() int {
	return i.ID
}

// GetType 获取VAD类型
func (i *SileroVADInstance) GetType() string {
	return SILERO_TYPE
}

// IsInUse 检查是否在使用中
func (i *SileroVADInstance) IsInUse() bool {
	return atomic.LoadInt32(&i.InUse) == 1
}

// SetInUse 设置使用状态
func (i *SileroVADInstance) SetInUse(inUse bool) {
	if inUse {
		atomic.StoreInt32(&i.InUse, 1)
	} else {
		atomic.StoreInt32(&i.InUse, 0)
	}
}

// GetLastUsed 获取最后使用时间
func (i *SileroVADInstance) GetLastUsed() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.LastUsed
}

// SetLastUsed 设置最后使用时间
func (i *SileroVADInstance) SetLastUsed(timestamp int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.LastUsed = timestamp
}

// Reset 重置实例状态
func (i *SileroVADInstance) Reset() error {
	if i.VAD != nil {
		// 清空Silero VAD缓冲区
		for !i.VAD.IsEmpty() {
			segment := i.VAD.Front()
			i.VAD.Pop()
			if segment != nil {
				// 释放segment资源（如果需要）
			}
		}
	}
	return nil
}

// Destroy 销毁实例
func (i *SileroVADInstance) Destroy() error {
	if i.VAD != nil {
		sherpa.DeleteVoiceActivityDetector(i.VAD)
		i.VAD = nil
		logger.Infof("🗑️ Silero VAD instance destroyed")
	}
	return nil
}

// SileroVADPool Silero VAD资源池
type SileroVADPool struct {
	instances []*SileroVADInstance
	available chan VADInstanceInterface
	config    *SileroVADConfig

	// 统计信息
	totalCreated int64
	totalReused  int64
	totalActive  int64

	// 控制
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSileroVADPool 创建新的Silero VAD资源池
func NewSileroVADPool(config *SileroVADConfig) *SileroVADPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &SileroVADPool{
		instances: make([]*SileroVADInstance, 0, config.PoolSize),
		available: make(chan VADInstanceInterface, config.PoolSize),
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
	}

	return pool
}

// Initialize 并行初始化VAD池
func (p *SileroVADPool) Initialize() error {
	logger.Infof("🔧 Initializing Silero VAD pool with %d instances...", p.config.PoolSize)

	// 并行初始化VAD实例
	var initWg sync.WaitGroup
	errorChan := make(chan error, p.config.PoolSize)

	for i := 0; i < p.config.PoolSize; i++ {
		initWg.Add(1)
		go func(instanceID int) {
			defer initWg.Done()

			// 创建VAD实例
			vad := sherpa.NewVoiceActivityDetector(p.config.ModelConfig, p.config.BufferSizeSeconds)
			if vad == nil {
				errorChan <- fmt.Errorf("failed to create Silero VAD instance %d", instanceID)
				return
			}

			instance := &SileroVADInstance{
				VAD:      vad,
				LastUsed: time.Now().UnixNano(),
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
				logger.Infof("✅ Silero VAD instance %d initialized", instanceID)
			default:
				// 队列满，销毁实例
				sherpa.DeleteVoiceActivityDetector(vad)
				errorChan <- fmt.Errorf("Silero VAD pool queue full, instance %d discarded", instanceID)
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
			logger.Warnf("⚠️ Silero VAD initialization warning: %v", err)
		}
	}

	successCount := len(p.instances)
	logger.Infof("🚀 Silero VAD pool initialized with %d/%d instances", successCount, p.config.PoolSize)

	if len(initErrors) > 0 && successCount == 0 {
		return fmt.Errorf("failed to initialize any Silero VAD instances")
	}

	return nil
}

// Get 获取VAD实例
func (p *SileroVADPool) Get() (VADInstanceInterface, error) {
	logger.Infof("🔍 Attempting to get Silero VAD instance from pool (available: %d)", len(p.available))

	select {
	case instance := <-p.available:
		logger.Infof("🎯 Got Silero VAD instance %d from pool", instance.GetID())
		if atomic.CompareAndSwapInt32(&instance.(*SileroVADInstance).InUse, 0, 1) {
			instance.SetLastUsed(time.Now().UnixNano())
			atomic.AddInt64(&p.totalReused, 1)
			atomic.AddInt64(&p.totalActive, 1)
			logger.Infof("✅ Silero VAD instance %d marked as in-use (active: %d)", instance.GetID(), atomic.LoadInt64(&p.totalActive))
			return instance, nil
		}
		// 实例已被使用，重新放回队列
		logger.Warnf("⚠️ Silero VAD instance %d already in use, returning to pool", instance.GetID())
		select {
		case p.available <- instance:
		default:
		}
		return p.Get() // 递归重试
	case <-time.After(100 * time.Millisecond):
		// 超时，创建新实例
		logger.Warnf("⏰ Silero VAD pool timeout, creating new temporary instance")
		return p.createNewInstance()
	case <-p.ctx.Done():
		logger.Errorf("❌ Silero VAD pool is shutting down")
		return nil, fmt.Errorf("Silero VAD pool is shutting down")
	}
}

// Put 归还VAD实例
func (p *SileroVADPool) Put(instance VADInstanceInterface) {
	if instance == nil {
		logger.Warnf("⚠️ Attempted to put nil Silero VAD instance")
		return
	}

	logger.Infof("🔄 Returning Silero VAD instance %d to pool", instance.GetID())

	if atomic.CompareAndSwapInt32(&instance.(*SileroVADInstance).InUse, 1, 0) {
		instance.SetLastUsed(time.Now().UnixNano())
		atomic.AddInt64(&p.totalActive, -1)
		logger.Infof("✅ Silero VAD instance %d marked as available (active: %d)", instance.GetID(), atomic.LoadInt64(&p.totalActive))

		// 重置VAD状态
		if err := instance.Reset(); err != nil {
			logger.Warnf("⚠️ Failed to reset Silero VAD instance %d: %v", instance.GetID(), err)
		}

		select {
		case p.available <- instance:
			// 成功归还
			logger.Infof("✅ Silero VAD instance %d returned to pool (available: %d)", instance.GetID(), len(p.available))
		default:
			// 队列满，销毁实例
			logger.Warnf("⚠️ Silero VAD pool queue full, destroying instance %d", instance.GetID())
			instance.Destroy()
		}
	} else {
		logger.Warnf("⚠️ Silero VAD instance %d was not in use, cannot return", instance.GetID())
	}
}

// createNewInstance 创建新的VAD实例
func (p *SileroVADPool) createNewInstance() (VADInstanceInterface, error) {
	vad := sherpa.NewVoiceActivityDetector(p.config.ModelConfig, p.config.BufferSizeSeconds)
	if vad == nil {
		return nil, fmt.Errorf("failed to create new Silero VAD instance")
	}

	instance := &SileroVADInstance{
		VAD:      vad,
		LastUsed: time.Now().UnixNano(),
		InUse:    1,
		ID:       -1, // 临时实例
	}

	atomic.AddInt64(&p.totalCreated, 1)
	atomic.AddInt64(&p.totalActive, 1)

	logger.Infof("🆕 Created temporary Silero VAD instance")
	return instance, nil
}

// GetStats 获取统计信息
func (p *SileroVADPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"vad_type":        SILERO_TYPE,
		"pool_size":       p.config.PoolSize,
		"max_idle":        p.config.MaxIdle,
		"total_instances": len(p.instances),
		"available_count": len(p.available),
		"active_count":    atomic.LoadInt64(&p.totalActive),
		"total_created":   atomic.LoadInt64(&p.totalCreated),
		"total_reused":    atomic.LoadInt64(&p.totalReused),
	}
}

// Shutdown 关闭VAD池
func (p *SileroVADPool) Shutdown() {
	logger.Infof("🛑 Shutting down Silero VAD pool...")

	// 取消上下文
	p.cancel()

	// 销毁所有实例
	p.mu.Lock()
	defer p.mu.Unlock()

	// 清空可用队列
	for {
		select {
		case instance := <-p.available:
			instance.Destroy()
		default:
			goto cleanup_instances
		}
	}

cleanup_instances:
	// 销毁所有实例
	for _, instance := range p.instances {
		instance.Destroy()
	}

	p.instances = nil
	close(p.available)

	logger.Infof("✅ Silero VAD pool shutdown complete")
}
