package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

var Logger *slog.Logger

// InitLogger 初始化日志系统，支持轮转和多输出
func InitLogger(level slog.Level, format, output, filePath string, maxSize, maxBackups, maxAge int, compress bool) {
	var writers []io.Writer
	if output == "console" || output == "both" {
		writers = append(writers, os.Stdout)
	}
	if output == "file" || output == "both" {
		writers = append(writers, &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   compress,
		})
	}
	mw := io.MultiWriter(writers...)
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(mw, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(mw, &slog.HandlerOptions{Level: level})
	}
	Logger = slog.New(handler)
}

func Info(msg string, args ...any) {
	Logger.Info(msg, args...)
}

func Infof(format string, args ...any) {
	Logger.Info(fmt.Sprintf(format, args...))
}

func Error(msg string, args ...any) {
	Logger.Error(msg, args...)
}

func Errorf(format string, args ...any) {
	Logger.Error(fmt.Sprintf(format, args...))
}

func Warn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}

func Warnf(format string, args ...any) {
	Logger.Warn(fmt.Sprintf(format, args...))
}

func Debug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

func Debugf(format string, args ...any) {
	Logger.Debug(fmt.Sprintf(format, args...))
}

type LoggingConfig struct {
	Level      string `json:"level"`
	Format     string `json:"format"`
	Output     string `json:"output"`
	FilePath   string `json:"file_path"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
	MaxAge     int    `json:"max_age"`
	Compress   bool   `json:"compress"`
}

func parseSlogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// InitLoggerFromConfig 直接用LoggingConfig结构体初始化logger
func InitLoggerFromConfig(cfg LoggingConfig) {
	InitLogger(
		parseSlogLevel(cfg.Level),
		cfg.Format,
		cfg.Output,
		cfg.FilePath,
		cfg.MaxSize,
		cfg.MaxBackups,
		cfg.MaxAge,
		cfg.Compress,
	)
}
