package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 全局日志实例
var Logger *logrus.Logger

// LogConfig 日志配置
type LogConfig struct {
	Level      string `json:"level"`       // 日志级别: debug, info, warn, error
	Format     string `json:"format"`      // 日志格式: json, text
	Output     string `json:"output"`      // 输出方式: console, file, both
	FilePath   string `json:"file_path"`   // 日志文件路径
	MaxSize    int    `json:"max_size"`    // 单个日志文件最大大小(MB)
	MaxBackups int    `json:"max_backups"` // 保留的旧日志文件数量
	MaxAge     int    `json:"max_age"`     // 日志文件保留天数
	Compress   bool   `json:"compress"`    // 是否压缩旧日志文件
}

// DefaultLogConfig 默认日志配置
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:      "info",
		Format:     "text",
		Output:     "both",
		FilePath:   "logs/app.log",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}
}

// CustomFormatter 自定义日志格式化器
type CustomFormatter struct {
	TimestampFormat string
}

// Format 格式化日志条目
func (f *CustomFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	timestamp := entry.Time.Format(f.TimestampFormat)

	// 使用简单的文本标识符而不是emoji，避免编码问题
	var levelText string
	switch entry.Level {
	case logrus.DebugLevel:
		levelText = "[DEBUG]"
	case logrus.InfoLevel:
		levelText = "[INFO] "
	case logrus.WarnLevel:
		levelText = "[WARN] "
	case logrus.ErrorLevel:
		levelText = "[ERROR]"
	case logrus.FatalLevel:
		levelText = "[FATAL]"
	case logrus.PanicLevel:
		levelText = "[PANIC]"
	default:
		levelText = "[LOG]  "
	}

	// 构建日志消息
	msg := timestamp + " " + levelText + " " + entry.Message

	// 添加字段信息
	if len(entry.Data) > 0 {
		msg += " |"
		for key, value := range entry.Data {
			msg += " " + key + "=" + formatValue(value)
		}
	}

	msg += "\n"
	return []byte(msg), nil
}

// formatValue 格式化字段值
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%.2f", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Duration:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// InitLogger 初始化日志系统
func InitLogger(config *LogConfig) error {
	Logger = logrus.New()

	// 设置日志级别
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	Logger.SetLevel(level)

	// 设置日志格式
	if config.Format == "json" {
		Logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
	} else {
		Logger.SetFormatter(&CustomFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
	}

	// 设置输出
	var writers []io.Writer

	// 控制台输出
	if config.Output == "console" || config.Output == "both" {
		writers = append(writers, os.Stdout)
	}

	// 文件输出
	if config.Output == "file" || config.Output == "both" {
		// 确保日志目录存在
		logDir := filepath.Dir(config.FilePath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}

		// 配置日志轮转
		fileWriter := &lumberjack.Logger{
			Filename:   config.FilePath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		writers = append(writers, fileWriter)
	}

	// 设置多输出
	if len(writers) > 1 {
		Logger.SetOutput(io.MultiWriter(writers...))
	} else if len(writers) == 1 {
		Logger.SetOutput(writers[0])
	}

	return nil
}

// GetLogger 获取日志实例
func GetLogger() *logrus.Logger {
	if Logger == nil {
		// 如果没有初始化，使用默认配置
		config := DefaultLogConfig()
		InitLogger(config)
	}
	return Logger
}

// WithFields 创建带字段的日志条目
func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}

// WithField 创建带单个字段的日志条目
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// WithError 创建带错误字段的日志条目
func WithError(err error) *logrus.Entry {
	return GetLogger().WithError(err)
}

// Fields 日志字段类型别名
type Fields = logrus.Fields

// Debug 调试日志
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// Debugf 格式化调试日志
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Info 信息日志
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Infof 格式化信息日志
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Warn 警告日志
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Warnf 格式化警告日志
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Error 错误日志
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Errorf 格式化错误日志
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatal 致命错误日志
func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

// Fatalf 格式化致命错误日志
func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}

// Panic panic日志
func Panic(args ...interface{}) {
	GetLogger().Panic(args...)
}

// Panicf 格式化panic日志
func Panicf(format string, args ...interface{}) {
	GetLogger().Panicf(format, args...)
}
