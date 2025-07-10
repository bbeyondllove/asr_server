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
	VADPool          pool.VADPoolInterface
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
		logger.Info("🔄 Log level changed to: %s", config.GlobalConfig.Logging.Level)
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
		logger.Error(fmt.Sprintf("Failed to initialize hot reload manager: %v", err))
		return nil, fmt.Errorf("failed to initialize hot reload manager: %v", err)
	}
	if err := hotReloadMgr.StartWatching("config.json"); err != nil {
		logger.Warn(fmt.Sprintf("Failed to start config file watching, continuing without hot reload: %v", err))
	}

	// 初始化全局识别器
	logger.Info("🔧 Initializing global recognizer...")
	globalRecognizer, err := createRecognizer(cfg)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to initialize global recognizer: %v", err))
		return nil, fmt.Errorf("failed to initialize global recognizer: %v", err)
	}

	// 根据VAD类型初始化VAD池
	var vadPool pool.VADPoolInterface
	vadFactory := pool.NewVADFactory()

	if config.GlobalConfig.VAD.Provider == pool.SILERO_TYPE {
		// 检查VAD模型文件是否存在（仅对silero需要）
		if _, err := os.Stat(cfg.VAD.SileroVAD.ModelPath); os.IsNotExist(err) {
			logger.Error(fmt.Sprintf("VAD model file not found, model_path=%s", cfg.VAD.SileroVAD.ModelPath))
			return nil, fmt.Errorf("VAD model file not found: %s", cfg.VAD.SileroVAD.ModelPath)
		}
	}

	// 使用工厂创建VAD池
	vadPool, err = vadFactory.CreateVADPool()
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create VAD pool: %v", err))
		return nil, fmt.Errorf("failed to create VAD pool: %v", err)
	}

	// 初始化VAD池
	logger.Info(fmt.Sprintf("🔧 Initializing VAD pool... pool_size=%d", cfg.VAD.PoolSize))
	if err := vadPool.Initialize(); err != nil {
		logger.Error(fmt.Sprintf("Failed to initialize VAD pool: %v", err))
		return nil, fmt.Errorf("failed to initialize VAD pool: %v", err)
	}

	// 初始化会话管理器
	logger.Info("🔧 Initializing session manager...")
	sessionManager := session.NewManager(globalRecognizer, vadPool)

	// 注册配置热加载回调
	registerHotReloadCallbacks(hotReloadMgr)

	// 初始化速率限制器
	logger.Info(fmt.Sprintf("🔧 Initializing rate limiter... requests_per_second=%d, max_connections=%d", cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.MaxConnections))
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
				logger.Warn(fmt.Sprintf("Failed to initialize speaker recognition module, continuing without it: %v", err))
			}
		} else {
			logger.Warn(fmt.Sprintf("Speaker model file not found, speaker recognition disabled, model_path=%s", cfg.Speaker.ModelPath))
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
