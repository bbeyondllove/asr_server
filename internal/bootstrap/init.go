package bootstrap

import (
	"fmt"
	"os"

	"asr_server/config"
	"asr_server/internal/config/hotreload"
	"asr_server/internal/logger"
	"asr_server/internal/middleware"
	"asr_server/internal/pool"
	"asr_server/internal/session"
	"asr_server/internal/speaker"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

type AppDependencies struct {
	SessionManager   *session.Manager
	VADPool          *pool.VADPool
	RateLimiter      *middleware.RateLimiter
	SpeakerManager   *speaker.Manager
	SpeakerHandler   *speaker.Handler
	GlobalRecognizer *sherpa.OfflineRecognizer
	HotReloadMgr     *hotreload.HotReloadManager
}

// createRecognizer 用于初始化 sherpa 识别器
func createRecognizer(cfg *config.Config) (*sherpa.OfflineRecognizer, error) {
	c := sherpa.OfflineRecognizerConfig{}
	c.FeatConfig.SampleRate = cfg.Audio.SampleRate
	c.FeatConfig.FeatureDim = cfg.Audio.FeatureDim

	c.ModelConfig.SenseVoice.Model = cfg.Recognition.ModelPath
	c.ModelConfig.Tokens = cfg.Recognition.TokensPath
	c.ModelConfig.NumThreads = cfg.Recognition.NumThreads
	c.ModelConfig.Debug = 0
	if cfg.Recognition.Debug {
		c.ModelConfig.Debug = 1
	}
	c.ModelConfig.Provider = cfg.Recognition.Provider

	recognizer := sherpa.NewOfflineRecognizer(&c)
	if recognizer == nil {
		return nil, fmt.Errorf("failed to create offline recognizer")
	}

	return recognizer, nil
}

// registerHotReloadCallbacks 注册配置热加载回调
func registerHotReloadCallbacks(hotReloadMgr *hotreload.HotReloadManager) {
	if hotReloadMgr == nil {
		return
	}

	hotReloadMgr.RegisterCallback("logging.level", func() {
		logger.Infof("🔄 Log level changed to: %s", config.GlobalConfig.Logging.Level)
	})
	hotReloadMgr.RegisterCallback("vad", func() {
		logger.Info("🔄 VAD configuration changed")
	})
	hotReloadMgr.RegisterCallback("session", func() {
		logger.Info("🔄 Session configuration changed")
	})
	hotReloadMgr.RegisterCallback("rate_limit", func() {
		logger.Info("🔄 Rate limit configuration changed")
	})
	hotReloadMgr.RegisterCallback("response", func() {
		logger.Info("🔄 Response configuration changed")
	})
	logger.Info("✅ Hot reload callbacks registered")
}

// InitApp 初始化所有核心组件，返回依赖注入结构体
func InitApp(cfg *config.Config) (*AppDependencies, error) {
	logger.Info("🔧 Initializing components...")

	// 初始化配置热加载管理器
	logger.Info("🔧 Initializing hot reload manager...")
	hotReloadMgr, err := hotreload.NewHotReloadManager()
	if err != nil {
		logger.WithError(err).Error("Failed to initialize hot reload manager")
		return nil, fmt.Errorf("failed to initialize hot reload manager: %v", err)
	}
	if err := hotReloadMgr.StartWatching("config.json"); err != nil {
		logger.WithError(err).Warn("Failed to start config file watching, continuing without hot reload")
	}

	// 检查VAD模型文件是否存在
	if _, err := os.Stat(cfg.VAD.ModelPath); os.IsNotExist(err) {
		logger.WithField("model_path", cfg.VAD.ModelPath).Error("VAD model file not found")
		return nil, fmt.Errorf("VAD model file not found: %s", cfg.VAD.ModelPath)
	}

	// 初始化全局识别器
	logger.Info("🔧 Initializing global recognizer...")
	globalRecognizer, err := createRecognizer(cfg)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize global recognizer")
		return nil, fmt.Errorf("failed to initialize global recognizer: %v", err)
	}

	// 创建VAD配置
	vadConfig := &sherpa.VadModelConfig{
		SileroVad: sherpa.SileroVadModelConfig{
			Model:              cfg.VAD.ModelPath,
			Threshold:          cfg.VAD.Threshold,
			MinSilenceDuration: cfg.VAD.MinSilenceDuration,
			MinSpeechDuration:  cfg.VAD.MinSpeechDuration,
			WindowSize:         cfg.VAD.WindowSize,
			MaxSpeechDuration:  cfg.VAD.MaxSpeechDuration,
		},
		SampleRate: cfg.Audio.SampleRate,
		NumThreads: cfg.Recognition.NumThreads,
		Provider:   cfg.Recognition.Provider,
		Debug:      0,
	}

	// 初始化VAD池
	logger.WithField("pool_size", cfg.VAD.PoolSize).Info("🔧 Initializing VAD pool...")
	vadPool := pool.NewVADPool(
		vadConfig,
		cfg.VAD.BufferSizeSeconds,
		cfg.VAD.PoolSize,
	)
	if err := vadPool.Initialize(); err != nil {
		logger.WithError(err).Error("Failed to initialize VAD pool")
		return nil, fmt.Errorf("failed to initialize VAD pool: %v", err)
	}

	// 初始化会话管理器
	logger.Info("🔧 Initializing session manager...")
	sessionManager := session.NewManager(globalRecognizer, vadPool)

	// 注册配置热加载回调
	registerHotReloadCallbacks(hotReloadMgr)

	// 初始化速率限制器
	logger.WithFields(logger.Fields{
		"requests_per_second": cfg.RateLimit.RequestsPerSecond,
		"max_connections":     cfg.RateLimit.MaxConnections,
	}).Info("🔧 Initializing rate limiter...")
	rateLimiter := middleware.NewRateLimiter(
		cfg.RateLimit.Enabled,
		cfg.RateLimit.RequestsPerSecond,
		cfg.RateLimit.BurstSize,
		cfg.RateLimit.MaxConnections,
	)

	// 初始化声纹识别模块
	var speakerManager *speaker.Manager
	var speakerHandler *speaker.Handler
	if cfg.Speaker.Enabled {
		if _, statErr := os.Stat(cfg.Speaker.ModelPath); !os.IsNotExist(statErr) {
			speakerConfig := &speaker.Config{
				ModelPath:  cfg.Speaker.ModelPath,
				NumThreads: cfg.Speaker.NumThreads,
				Provider:   cfg.Speaker.Provider,
				Threshold:  cfg.Speaker.Threshold,
				DataDir:    cfg.Speaker.DataDir,
			}
			mgr, err := speaker.NewManager(speakerConfig)
			if err == nil {
				speakerManager = mgr
				speakerHandler = speaker.NewHandler(speakerManager)
			} else {
				logger.WithError(err).Warn("Failed to initialize speaker recognition module, continuing without it")
			}
		} else {
			logger.WithField("model_path", cfg.Speaker.ModelPath).Warn("Speaker model file not found, speaker recognition disabled")
		}
	}

	logger.Info("✅ All components initialized successfully")
	return &AppDependencies{
		SessionManager:   sessionManager,
		VADPool:          vadPool,
		RateLimiter:      rateLimiter,
		SpeakerManager:   speakerManager,
		SpeakerHandler:   speakerHandler,
		GlobalRecognizer: globalRecognizer,
		HotReloadMgr:     hotReloadMgr,
	}, nil
}
