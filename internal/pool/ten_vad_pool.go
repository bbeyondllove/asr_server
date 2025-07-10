package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"asr_server/internal/logger"
)

// TenVADConfig TEN-VAD配置
type TenVADConfig struct {
	HopSize   int
	Threshold float32
	PoolSize  int
	MaxIdle   int
}

// TenVADInstance TEN-VAD实例
type TenVADInstance struct {
	ID       int
	Handle   unsafe.Pointer
	LastUsed int64
	InUse    int32
	mu       sync.RWMutex
}

// GetID 获取实例ID
func (i *TenVADInstance) GetID() int {
	return i.ID
}

// GetType 获取VAD类型
func (i *TenVADInstance) GetType() string {
	return TEN_VAD_TYPE
}

// IsInUse 检查是否在使用中
func (i *TenVADInstance) IsInUse() bool {
	return atomic.LoadInt32(&i.InUse) == 1
}

// SetInUse 设置使用状态
func (i *TenVADInstance) SetInUse(inUse bool) {
	if inUse {
		atomic.StoreInt32(&i.InUse, 1)
	} else {
		atomic.StoreInt32(&i.InUse, 0)
	}
}

// GetLastUsed 获取最后使用时间
func (i *TenVADInstance) GetLastUsed() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.LastUsed
}

// SetLastUsed 设置最后使用时间
func (i *TenVADInstance) SetLastUsed(timestamp int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.LastUsed = timestamp
}

// Reset 重置实例状态
func (i *TenVADInstance) Reset() error {
	// TEN-VAD不需要重置，每次处理都是独立的
	return nil
}

// Destroy 销毁实例
func (i *TenVADInstance) Destroy() error {
	if i.Handle != nil {
		tenVAD := GetInstance()
		tenVAD.DestroyInstance(i.Handle)
		i.Handle = nil
		logger.Info(fmt.Sprintf("🗑️ TEN-VAD instance %d destroyed", i.ID))
	}
	return nil
}

// TenVADPool TEN-VAD资源池
type TenVADPool struct {
	instances []*TenVADInstance
	available chan VADInstanceInterface
	config    *TenVADConfig

	// 统计信息
	totalCreated int64
	totalReused  int64
	totalActive  int64

	// 控制
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewTenVADPool 创建新的TEN-VAD资源池
func NewTenVADPool(config *TenVADConfig) *TenVADPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &TenVADPool{
		instances: make([]*TenVADInstance, 0, config.PoolSize),
		available: make(chan VADInstanceInterface, config.PoolSize),
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
	}

	return pool
}

// Initialize 并行初始化VAD池
func (p *TenVADPool) Initialize() error {
	logger.Info(fmt.Sprintf("🔧 Initializing TEN-VAD pool with %d instances...", p.config.PoolSize))

	// 并行初始化VAD实例
	var initWg sync.WaitGroup
	errorChan := make(chan error, p.config.PoolSize)

	for i := 0; i < p.config.PoolSize; i++ {
		initWg.Add(1)
		go func(instanceID int) {
			defer initWg.Done()

			// 创建TEN-VAD实例
			tenVAD := GetInstance()
			handle, err := tenVAD.CreateInstance(p.config.HopSize, p.config.Threshold)
			if err != nil {
				errorChan <- fmt.Errorf("failed to create TEN-VAD instance %d: %v", instanceID, err)
				return
			}

			instance := &TenVADInstance{
				Handle:   handle,
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
				logger.Info(fmt.Sprintf("✅ TEN-VAD instance %d initialized", instanceID))
			default:
				// 队列满，销毁实例
				tenVAD.DestroyInstance(handle)
				errorChan <- fmt.Errorf("TEN-VAD pool queue full, instance %d discarded", instanceID)
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
			logger.Warn("⚠️ TEN-VAD initialization warning: %v", err)
		}
	}

	successCount := len(p.instances)
	logger.Info(fmt.Sprintf("🚀 TEN-VAD pool initialized with %d/%d instances", successCount, p.config.PoolSize))

	if len(initErrors) > 0 && successCount == 0 {
		return fmt.Errorf("failed to initialize any TEN-VAD instances")
	}

	return nil
}

// Get 获取VAD实例
func (p *TenVADPool) Get() (VADInstanceInterface, error) {
	logger.Info(fmt.Sprintf("🔍 Attempting to get TEN-VAD instance from pool (available: %d)", len(p.available)))

	select {
	case instance := <-p.available:
		logger.Info(fmt.Sprintf("🎯 Got TEN-VAD instance %d from pool", instance.GetID()))
		if atomic.CompareAndSwapInt32(&instance.(*TenVADInstance).InUse, 0, 1) {
			instance.SetLastUsed(time.Now().UnixNano())
			atomic.AddInt64(&p.totalReused, 1)
			atomic.AddInt64(&p.totalActive, 1)
			logger.Info(fmt.Sprintf("✅ TEN-VAD instance %d marked as in-use (active: %d)", instance.GetID(), atomic.LoadInt64(&p.totalActive)))
			return instance, nil
		}
		// 实例已被使用，重新放回队列
		logger.Warn(fmt.Sprintf("⚠️ TEN-VAD instance %d already in use, returning to pool", instance.GetID()))
		select {
		case p.available <- instance:
		default:
		}
		return p.Get() // 递归重试
	case <-time.After(100 * time.Millisecond):
		// 超时，创建新实例
		logger.Warn("⏰ TEN-VAD pool timeout, creating new temporary instance")
		return p.createNewInstance()
	case <-p.ctx.Done():
		logger.Error("❌ TEN-VAD pool is shutting down")
		return nil, fmt.Errorf("TEN-VAD pool is shutting down")
	}
}

// Put 归还VAD实例
func (p *TenVADPool) Put(instance VADInstanceInterface) {
	if instance == nil {
		logger.Warn("⚠️ Attempted to put nil TEN-VAD instance")
		return
	}

	logger.Info(fmt.Sprintf("🔄 Returning TEN-VAD instance %d to pool", instance.GetID()))

	if atomic.CompareAndSwapInt32(&instance.(*TenVADInstance).InUse, 1, 0) {
		instance.SetLastUsed(time.Now().UnixNano())
		atomic.AddInt64(&p.totalActive, -1)
		logger.Info(fmt.Sprintf("✅ TEN-VAD instance %d marked as available (active: %d)", instance.GetID(), atomic.LoadInt64(&p.totalActive)))

		// 重置VAD状态
		if err := instance.Reset(); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to reset TEN-VAD instance %d: %v", instance.GetID(), err))
		}

		select {
		case p.available <- instance:
			// 成功归还
			logger.Info(fmt.Sprintf("✅ TEN-VAD instance %d returned to pool (available: %d)", instance.GetID(), len(p.available)))
		default:
			// 队列满，销毁实例
			logger.Warn(fmt.Sprintf("⚠️ TEN-VAD pool queue full, destroying instance %d", instance.GetID()))
			instance.Destroy()
		}
	} else {
		logger.Warn(fmt.Sprintf("⚠️ TEN-VAD instance %d was not in use, cannot return", instance.GetID()))
	}
}

// createNewInstance 创建新的VAD实例
func (p *TenVADPool) createNewInstance() (VADInstanceInterface, error) {
	tenVAD := GetInstance()
	handle, err := tenVAD.CreateInstance(p.config.HopSize, p.config.Threshold)
	if err != nil {
		return nil, fmt.Errorf("failed to create new TEN-VAD instance: %v", err)
	}

	instance := &TenVADInstance{
		Handle:   handle,
		LastUsed: time.Now().UnixNano(),
		InUse:    1,
		ID:       -1, // 临时实例
	}

	atomic.AddInt64(&p.totalCreated, 1)
	atomic.AddInt64(&p.totalActive, 1)

	logger.Info("🆕 Created temporary TEN-VAD instance")
	return instance, nil
}

// GetStats 获取统计信息
func (p *TenVADPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"vad_type":        TEN_VAD_TYPE,
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
func (p *TenVADPool) Shutdown() {
	logger.Info("🛑 Shutting down TEN-VAD pool...")

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

	logger.Info("✅ TEN-VAD pool shutdown complete")
}
