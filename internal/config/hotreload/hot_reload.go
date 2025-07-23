package hotreload

import (
	"fmt"
	"sync"
	"time"

	"asr_server/config"
	"asr_server/internal/logger"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// HotReloadManager 配置热加载管理器
type HotReloadManager struct {
	mu            sync.RWMutex
	callbacks     map[string][]func()
	watcher       *fsnotify.Watcher
	debounceTimer *time.Timer
	stopChan      chan struct{}
}

// NewHotReloadManager 创建新的热加载管理器
func NewHotReloadManager() (*HotReloadManager, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	manager := &HotReloadManager{
		callbacks: make(map[string][]func()),
		watcher:   watcher,
		stopChan:  make(chan struct{}),
	}

	return manager, nil
}

// RegisterCallback 注册配置变更回调
func (m *HotReloadManager) RegisterCallback(configKey string, callback func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.callbacks[configKey] == nil {
		m.callbacks[configKey] = make([]func(), 0)
	}
	m.callbacks[configKey] = append(m.callbacks[configKey], callback)
}

// StartWatching 开始监听配置文件
func (m *HotReloadManager) StartWatching(configPath string) error {
	// 添加配置文件到监听列表
	if err := m.watcher.Add(configPath); err != nil {
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	// 启动监听协程
	go m.watchLoop()

	logger.Infof("🔍 Started watching config file: %s", configPath)
	return nil
}

// watchLoop 监听循环
func (m *HotReloadManager) watchLoop() {
	defer m.watcher.Close()

	for {
		select {
		case event := <-m.watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				m.handleConfigChange()
			}
		case err := <-m.watcher.Errors:
			logger.Errorf("❌ Config file watcher error: %v", err)
		case <-m.stopChan:
			logger.Infof("🛑 Config file watcher stopped")
			return
		}
	}
}

// handleConfigChange 处理配置文件变更
func (m *HotReloadManager) handleConfigChange() {
	// 防抖动处理
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}

	m.debounceTimer = time.AfterFunc(2*time.Second, func() {
		m.reloadConfig()
	})
}

// reloadConfig 重新加载配置
func (m *HotReloadManager) reloadConfig() {
	logger.Infof("🔄 Reloading configuration...")

	// 重新读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		logger.Errorf("❌ Failed to read config file: %v", err)
		return
	}

	// 重新解析配置
	if err := viper.Unmarshal(&config.GlobalConfig); err != nil {
		logger.Errorf("❌ Failed to unmarshal config: %v", err)
		return
	}

	logger.Infof("✅ Configuration reloaded successfully")

	// 执行回调函数
	m.executeCallbacks()
}

// executeCallbacks 执行回调函数
func (m *HotReloadManager) executeCallbacks() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for configKey, callbacks := range m.callbacks {
		logger.Infof("🔄 Executing callbacks for config key: %s", configKey)
		for _, callback := range callbacks {
			// 在goroutine中执行回调，避免阻塞
			go func(cb func()) {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("❌ Callback panicked: %v", r)
					}
				}()
				cb()
			}(callback)
		}
	}
}

// Stop 停止监听
func (m *HotReloadManager) Stop() {
	close(m.stopChan)
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
}

// GetConfigValue 获取配置值
func (m *HotReloadManager) GetConfigValue(key string) interface{} {
	return viper.Get(key)
}

// SetConfigValue 设置配置值
func (m *HotReloadManager) SetConfigValue(key string, value interface{}) error {
	viper.Set(key, value)

	// 重新解析到结构体
	if err := viper.Unmarshal(&config.GlobalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 执行相关回调
	m.executeCallbacks()

	return nil
}

// SaveConfig 保存配置到文件
func (m *HotReloadManager) SaveConfig() error {
	return viper.WriteConfig()
}
