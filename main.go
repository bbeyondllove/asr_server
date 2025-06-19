package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"asr_server/config"
	"asr_server/internal/logger"
	"asr_server/internal/middleware"
	"asr_server/internal/pool"
	"asr_server/internal/session"
	"asr_server/internal/speaker"

	"github.com/sirupsen/logrus"
)

var (
	upgrader       = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	resourcePool   pool.Pool // 使用接口类型，支持StreamPool和ResourcePool
	vadPool        *pool.VADPool
	sessionManager *session.Manager
	rateLimiter    *middleware.RateLimiter
	speakerManager *speaker.Manager
	speakerHandler *speaker.Handler
	ginRouter      *gin.Engine
)

// generateSessionID 生成会话ID
func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// initializeComponents 初始化所有组件
func initializeComponents() error {
	logger.Info("🔧 Initializing components...")

	// 初始化Stream资源池 (参考go-sherpa-server架构)
	logger.WithField("worker_count", config.GlobalConfig.Pool.WorkerCount).Info("🔧 Initializing stream pool...")
	streamPool, err := pool.NewStreamPool(&config.GlobalConfig, logger.Logger)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize stream pool")
		return fmt.Errorf("failed to initialize stream pool: %v", err)
	}
	resourcePool = streamPool

	// 创建VAD配置
	vadConfig := &sherpa.VadModelConfig{
		SileroVad: sherpa.SileroVadModelConfig{
			Model:              config.GlobalConfig.VAD.ModelPath,
			Threshold:          config.GlobalConfig.VAD.Threshold,
			MinSilenceDuration: config.GlobalConfig.VAD.MinSilenceDuration,
			MinSpeechDuration:  config.GlobalConfig.VAD.MinSpeechDuration,
			WindowSize:         config.GlobalConfig.VAD.WindowSize,
			MaxSpeechDuration:  config.GlobalConfig.VAD.MaxSpeechDuration,
		},
		SampleRate: config.GlobalConfig.Audio.SampleRate,
		NumThreads: config.GlobalConfig.Recognition.NumThreads,
		Provider:   config.GlobalConfig.Recognition.Provider,
		Debug:      0,
	}

	// 初始化VAD池
	logger.WithField("pool_size", config.GlobalConfig.VADPool.PoolSize).Info("🔧 Initializing VAD pool...")
	vadPool = pool.NewVADPool(
		vadConfig,
		config.GlobalConfig.VAD.BufferSizeSeconds,
		config.GlobalConfig.VADPool.PoolSize,
		config.GlobalConfig.VADPool.MaxIdle,
		time.Duration(config.GlobalConfig.VADPool.CleanupInterval)*time.Second,
	)

	if err := vadPool.Initialize(); err != nil {
		logger.WithError(err).Error("Failed to initialize VAD pool")
		return fmt.Errorf("failed to initialize VAD pool: %v", err)
	}

	// 初始化会话管理器
	logger.Info("🔧 Initializing session manager...")
	sessionManager = session.NewManager(resourcePool, vadPool)

	// 初始化速率限制器
	logger.WithFields(logger.Fields{
		"requests_per_second": config.GlobalConfig.RateLimit.RequestsPerSecond,
		"max_connections":     config.GlobalConfig.RateLimit.MaxConnections,
	}).Info("🔧 Initializing rate limiter...")
	rateLimiter = middleware.NewRateLimiter(
		config.GlobalConfig.RateLimit.RequestsPerSecond,
		config.GlobalConfig.RateLimit.BurstSize,
		config.GlobalConfig.RateLimit.MaxConnections,
	)

	// 初始化声纹识别模块
	if err := initializeSpeakerModule(); err != nil {
		logger.WithError(err).Warn("Failed to initialize speaker recognition module, continuing without it")
		speakerManager = nil
		speakerHandler = nil
	}

	logger.Info("✅ All components initialized successfully")
	return nil
}

// initializeSpeakerModule 初始化声纹识别模块
func initializeSpeakerModule() error {
	logger.Info("🔧 Initializing speaker recognition module...")

	// 检查声纹识别配置
	if !config.GlobalConfig.Speaker.Enabled {
		logger.Info("Speaker recognition is disabled in configuration")
		return fmt.Errorf("speaker recognition disabled")
	}

	// 检查模型文件是否存在
	if _, err := os.Stat(config.GlobalConfig.Speaker.ModelPath); os.IsNotExist(err) {
		logger.WithField("model_path", config.GlobalConfig.Speaker.ModelPath).
			Warn("Speaker model file not found, speaker recognition disabled")
		return fmt.Errorf("speaker model file not found: %s", config.GlobalConfig.Speaker.ModelPath)
	}

	// 创建声纹识别配置
	speakerConfig := &speaker.Config{
		ModelPath:  config.GlobalConfig.Speaker.ModelPath,
		NumThreads: config.GlobalConfig.Speaker.NumThreads,
		Provider:   config.GlobalConfig.Speaker.Provider,
		Threshold:  config.GlobalConfig.Speaker.Threshold,
		DataDir:    config.GlobalConfig.Speaker.DataDir,
	}

	// 创建声纹识别管理器
	var err error
	speakerManager, err = speaker.NewManager(speakerConfig)
	if err != nil {
		return fmt.Errorf("failed to create speaker manager: %v", err)
	}

	// 创建HTTP处理器
	speakerHandler = speaker.NewHandler(speakerManager)

	logger.WithFields(logger.Fields{
		"model_path": config.GlobalConfig.Speaker.ModelPath,
		"data_dir":   config.GlobalConfig.Speaker.DataDir,
		"threshold":  config.GlobalConfig.Speaker.Threshold,
	}).Info("✅ Speaker recognition module initialized")

	return nil
}

// handleWebSocket WebSocket处理函数
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.WithError(err).Error("WebSocket upgrade failed")
		return
	}

	// 生成会话ID
	sessionID := generateSessionID()

	// 创建会话
	_, err = sessionManager.CreateSession(sessionID, conn)
	if err != nil {
		logger.WithFields(logger.Fields{
			"session_id": sessionID,
			"error":      err,
		}).Error("Failed to create session")
		conn.Close()
		return
	}

	defer func() {
		sessionManager.RemoveSession(sessionID)
		logger.WithField("session_id", sessionID).Info("WebSocket connection closed")
	}()

	logger.WithField("session_id", sessionID).Info("New WebSocket connection established")

	// 发送连接确认
	conn.WriteJSON(map[string]interface{}{
		"type":       "connection",
		"message":    "WebSocket connected, ready for audio",
		"session_id": sessionID,
	})

	// 处理消息
	for {
		// 读取消息
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.WithFields(logger.Fields{
				"session_id": sessionID,
				"error":      err,
			}).Warn("WebSocket read error")
			break
		}

		// 处理音频数据
		if len(message) > 0 {
			if err := sessionManager.ProcessAudioData(sessionID, message); err != nil {
				logger.WithFields(logger.Fields{
					"session_id": sessionID,
					"error":      err,
				}).Error("Failed to process audio data")
				// 发送错误消息
				conn.WriteJSON(map[string]interface{}{
					"type":    "error",
					"message": err.Error(),
				})
			}
		}
	}
}

// healthHandler 健康检查
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	poolStats := resourcePool.GetStats()
	vadPoolStats := vadPool.GetStats()
	sessionStats := sessionManager.GetStats()
	rateLimiterStats := rateLimiter.GetStats()

	health := map[string]interface{}{
		"status":     "healthy",
		"timestamp":  time.Now().Format(time.RFC3339),
		"pool":       poolStats,
		"vad_pool":   vadPoolStats,
		"sessions":   sessionStats,
		"rate_limit": rateLimiterStats,
	}

	json.NewEncoder(w).Encode(health)
}

// statsHandler 统计信息
func statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats := map[string]interface{}{
		"pool":       resourcePool.GetStats(),
		"vad_pool":   vadPool.GetStats(),
		"sessions":   sessionManager.GetStats(),
		"rate_limit": rateLimiter.GetStats(),
		"timestamp":  time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(stats)
}

// infoHandler 服务信息
func infoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	info := map[string]interface{}{
		"service":   "ASR Server with Speaker Recognition",
		"version":   "1.0.0",
		"timestamp": time.Now().Format(time.RFC3339),
		"features": map[string]bool{
			"asr":             true,
			"vad":             vadPool != nil,
			"speaker":         speakerManager != nil,
			"rate_limiting":   rateLimiter != nil,
			"session_manager": sessionManager != nil,
		},
		"endpoints": []string{
			"/ws",
			"/health",
			"/stats",
			"/info",
		},
	}

	// 如果启用了声纹识别，添加相关端点
	if speakerHandler != nil {
		endpoints := info["endpoints"].([]string)
		speakerEndpoints := []string{
			"/api/v1/speaker/register",
			"/api/v1/speaker/identify",
			"/api/v1/speaker/verify/:speaker_id",
			"/api/v1/speaker/list",
			"/api/v1/speaker/stats",
			"/api/v1/speaker/:speaker_id",
		}
		info["endpoints"] = append(endpoints, speakerEndpoints...)
	}

	json.NewEncoder(w).Encode(info)
}

// metricsHandler 监控指标
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	metrics := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"uptime":    time.Since(time.Now().Add(-time.Hour)), // 简化的运行时间
	}

	if resourcePool != nil {
		metrics["pool"] = resourcePool.GetStats()
	}
	if vadPool != nil {
		metrics["vad_pool"] = vadPool.GetStats()
	}
	if sessionManager != nil {
		metrics["sessions"] = sessionManager.GetStats()
	}
	if rateLimiter != nil {
		metrics["rate_limit"] = rateLimiter.GetStats()
	}
	if speakerManager != nil {
		metrics["speaker"] = speakerManager.GetStats()
	}

	json.NewEncoder(w).Encode(metrics)
}

// gracefulShutdown 优雅关闭
func gracefulShutdown(server *http.Server) {
	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("🛑 Shutting down server...")

	// 设置关闭超时
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 关闭HTTP服务器
	if err := server.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
	}

	// 关闭组件
	if sessionManager != nil {
		logger.Info("🛑 Shutting down session manager...")
		sessionManager.Shutdown()
	}
	if vadPool != nil {
		logger.Info("🛑 Shutting down VAD pool...")
		vadPool.Shutdown()
	}
	if resourcePool != nil {
		logger.Info("🛑 Shutting down resource pool...")
		resourcePool.Shutdown()
	}
	if speakerManager != nil {
		logger.Info("🛑 Shutting down speaker manager...")
		speakerManager.Close()
	}

	logger.Info("✅ Server shutdown complete")
}

func main() {
	logger.Info("🚀 Starting VAD ASR Server (Optimized)...")

	// 加载配置
	logger.Info("📋 Loading configuration...")
	if err := config.InitConfig("config.json"); err != nil {
		logger.WithError(err).Fatal("Failed to load configuration")
	}

	// 设置日志级别
	level, err := logrus.ParseLevel(config.GlobalConfig.Logging.Level)
	if err != nil {
		logger.Warnf("无效的日志级别 '%s'，使用默认级别 'info'", config.GlobalConfig.Logging.Level)
		level = logrus.InfoLevel
	}

	// 临时设置为Debug级别以便调试崩溃问题
	if level > logrus.DebugLevel {
		level = logrus.DebugLevel
		logger.Info("临时启用Debug日志级别以调试崩溃问题")
	}

	// 使用正确的方法设置日志级别
	if logger.Logger != nil {
		logger.Logger.SetLevel(level)
	}

	// 初始化日志系统
	logConfig := &logger.LogConfig{
		Level:      config.GlobalConfig.Logging.Level,
		Format:     config.GlobalConfig.Logging.Format,
		Output:     config.GlobalConfig.Logging.Output,
		FilePath:   config.GlobalConfig.Logging.FilePath,
		MaxSize:    config.GlobalConfig.Logging.MaxSize,
		MaxBackups: config.GlobalConfig.Logging.MaxBackups,
		MaxAge:     config.GlobalConfig.Logging.MaxAge,
		Compress:   config.GlobalConfig.Logging.Compress,
	}

	if err := logger.InitLogger(logConfig); err != nil {
		logger.WithError(err).Fatal("Failed to initialize logger")
	}

	logger.Info("✅ Configuration loaded")
	config.PrintConfig()

	// 监听配置文件变化
	config.WatchConfig(func() {
		logger.Info("🔄 Configuration reloaded")
		config.PrintConfig()
	})

	// 初始化组件
	if err := initializeComponents(); err != nil {
		logger.WithError(err).Fatal("Failed to initialize components")
	}

	// 设置Gin模式
	if config.GlobalConfig.Recognition.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建Gin路由器
	ginRouter = gin.New()
	ginRouter.Use(gin.Recovery())
	if config.GlobalConfig.Recognition.Debug {
		ginRouter.Use(gin.Logger())
	}

	// 注册基础路由
	ginRouter.GET("/ws", gin.WrapF(handleWebSocket))
	ginRouter.GET("/health", gin.WrapF(healthHandler))
	ginRouter.GET("/stats", gin.WrapF(statsHandler))

	// 静态文件服务
	ginRouter.Static("/static", "./static")
	ginRouter.StaticFile("/", "./static/index.html")

	// 注册声纹识别路由（如果启用）
	if speakerHandler != nil {
		speakerHandler.RegisterRoutes(ginRouter)
		logger.Info("✅ Speaker recognition routes registered")
	}

	// 应用限流中间件（转换为Gin中间件）
	handler := rateLimiter.Middleware(ginRouter)

	// 创建HTTP服务器
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port),
		Handler:      handler,
		ReadTimeout:  time.Duration(config.GlobalConfig.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(config.GlobalConfig.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(config.GlobalConfig.Server.IdleTimeout) * time.Second,
	}

	// 启动优雅关闭协程
	go gracefulShutdown(server)

	// 启动服务器
	logger.WithFields(logger.Fields{
		"host": config.GlobalConfig.Server.Host,
		"port": config.GlobalConfig.Server.Port,
	}).Info("🎤 VAD ASR Server (Optimized) is running")
	logger.Infof("🔗 WebSocket: ws://%s:%d/ws", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port)
	logger.Infof("📊 Health check: http://%s:%d/health", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port)
	logger.Infof("📈 Statistics: http://%s:%d/stats", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port)
	logger.Infof("🧪 Test page: http://%s:%d/", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.WithError(err).Fatal("Server failed to start")
	}
}
