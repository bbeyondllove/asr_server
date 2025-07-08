package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"asr_server/config"
	"asr_server/internal/bootstrap"
	"asr_server/internal/logger"
	"asr_server/internal/router"

	"github.com/sirupsen/logrus"
)

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

	// 初始化所有依赖
	deps, err := bootstrap.InitApp(&config.GlobalConfig)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize app dependencies")
	}

	// 统一注册所有路由
	r := router.NewRouter(deps)

	// 创建HTTP服务器
	server := &http.Server{
		Addr:        fmt.Sprintf("%s:%d", config.GlobalConfig.Server.Host, config.GlobalConfig.Server.Port),
		Handler:     deps.RateLimiter.Middleware(r),
		ReadTimeout: time.Duration(config.GlobalConfig.Server.ReadTimeout) * time.Second,
	}

	// 优雅关闭
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		logger.Info("🛑 Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.WithError(err).Error("Server forced to shutdown")
		} else {
			logger.Info("✅ Server exited")
		}
		os.Exit(0)
	}()

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
