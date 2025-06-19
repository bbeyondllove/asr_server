package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config 配置结构
type Config struct {
	Server struct {
		Port           int    `mapstructure:"port"`
		Host           string `mapstructure:"host"`
		MaxConnections int    `mapstructure:"max_connections"`
		ReadTimeout    int    `mapstructure:"read_timeout"`
		WriteTimeout   int    `mapstructure:"write_timeout"`
		IdleTimeout    int    `mapstructure:"idle_timeout"`
	} `mapstructure:"server"`
	VAD struct {
		ModelPath          string  `mapstructure:"model_path"`
		Threshold          float32 `mapstructure:"threshold"`
		MinSilenceDuration float32 `mapstructure:"min_silence_duration"`
		MinSpeechDuration  float32 `mapstructure:"min_speech_duration"`
		MaxSpeechDuration  float32 `mapstructure:"max_speech_duration"`
		WindowSize         int     `mapstructure:"window_size"`
		BufferSizeSeconds  float32 `mapstructure:"buffer_size_seconds"`
	} `mapstructure:"vad"`
	Recognition struct {
		ModelPath                   string `mapstructure:"model_path"`
		TokensPath                  string `mapstructure:"tokens_path"`
		Language                    string `mapstructure:"language"`
		UseInverseTextNormalization bool   `mapstructure:"use_inverse_text_normalization"`
		NumThreads                  int    `mapstructure:"num_threads"`
		Provider                    string `mapstructure:"provider"`
		Debug                       bool   `mapstructure:"debug"`
	} `mapstructure:"recognition"`
	Audio struct {
		SampleRate int `mapstructure:"sample_rate"`
		FeatureDim int `mapstructure:"feature_dim"`
		QueueSize  int `mapstructure:"queue_size"`
	} `mapstructure:"audio"`
	Pool struct {
		InstanceMode string `mapstructure:"instance_mode"`
		WorkerCount  int    `mapstructure:"worker_count"`
		QueueSize    int    `mapstructure:"queue_size"`
		MaxRetries   int    `mapstructure:"max_retries"`
		RetryDelay   int    `mapstructure:"retry_delay"`
	} `mapstructure:"pool"`
	VADPool struct {
		PoolSize        int `mapstructure:"pool_size"`
		MaxIdle         int `mapstructure:"max_idle"`
		CleanupInterval int `mapstructure:"cleanup_interval"`
	} `mapstructure:"vad_pool"`
	RateLimit struct {
		RequestsPerSecond int `mapstructure:"requests_per_second"`
		BurstSize         int `mapstructure:"burst_size"`
		MaxConnections    int `mapstructure:"max_connections"`
	} `mapstructure:"rate_limit"`
	Response struct {
		SendMode        string `mapstructure:"send_mode"`
		QueueBufferSize int    `mapstructure:"queue_buffer_size"`
		FallbackToSync  bool   `mapstructure:"fallback_to_sync"`
		MaxRetryCount   int    `mapstructure:"max_retry_count"`
		TimeoutMs       int    `mapstructure:"timeout_ms"`
	} `mapstructure:"response"`

	Speaker struct {
		Enabled    bool    `mapstructure:"enabled"`
		ModelPath  string  `mapstructure:"model_path"`
		NumThreads int     `mapstructure:"num_threads"`
		Provider   string  `mapstructure:"provider"`
		Threshold  float32 `mapstructure:"threshold"`
		DataDir    string  `mapstructure:"data_dir"`
	} `mapstructure:"speaker"`
	Logging struct {
		Level      string `mapstructure:"level"`
		Format     string `mapstructure:"format"`
		Output     string `mapstructure:"output"`
		FilePath   string `mapstructure:"file_path"`
		MaxSize    int    `mapstructure:"max_size"`
		MaxBackups int    `mapstructure:"max_backups"`
		MaxAge     int    `mapstructure:"max_age"`
		Compress   bool   `mapstructure:"compress"`
	} `mapstructure:"logging"`
}

var GlobalConfig Config

// InitConfig 初始化配置
func InitConfig(configPath string) error {
	// 设置配置文件名和路径
	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("json")
		viper.AddConfigPath(".")
		viper.AddConfigPath("./config")
		viper.AddConfigPath("/etc/asr_server/")
	}

	// 设置环境变量前缀
	viper.SetEnvPrefix("VAD_ASR")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// 设置默认值
	setDefaults()

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 配置文件未找到，使用默认值
			fmt.Println("⚠️  Config file not found, using defaults")
		} else {
			// 配置文件找到但读取出错
			return fmt.Errorf("error reading config file: %w", err)
		}
	} else {
		fmt.Printf("✅ Using config file: %s\n", viper.ConfigFileUsed())
	}

	// 将配置解析到结构体
	if err := viper.Unmarshal(&GlobalConfig); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	return nil
}

// setDefaults 设置默认配置值
func setDefaults() {
	// Server defaults
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.host", "localhost")
	viper.SetDefault("server.max_connections", 50)
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 30)
	viper.SetDefault("server.idle_timeout", 120)

	// VAD defaults
	viper.SetDefault("vad.model_path", "models/silero_vad.onnx")
	viper.SetDefault("vad.threshold", 0.5)
	viper.SetDefault("vad.min_silence_duration", 0.5)
	viper.SetDefault("vad.min_speech_duration", 0.25)
	viper.SetDefault("vad.max_speech_duration", 30.0)
	viper.SetDefault("vad.window_size", 512)
	viper.SetDefault("vad.buffer_size_seconds", 30.0)

	// Recognition defaults
	viper.SetDefault("recognition.model_path", "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/model.onnx")
	viper.SetDefault("recognition.tokens_path", "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/tokens.txt")
	viper.SetDefault("recognition.language", "auto")
	viper.SetDefault("recognition.use_inverse_text_normalization", false)
	viper.SetDefault("recognition.num_threads", 4)
	viper.SetDefault("recognition.provider", "cpu")
	viper.SetDefault("recognition.debug", false)

	// Audio defaults
	viper.SetDefault("audio.sample_rate", 16000)
	viper.SetDefault("audio.feature_dim", 80)
	viper.SetDefault("audio.queue_size", 1000)

	// Pool defaults
	viper.SetDefault("pool.instance_mode", "single")
	viper.SetDefault("pool.worker_count", 4)
	viper.SetDefault("pool.queue_size", 50)
	viper.SetDefault("pool.max_retries", 3)
	viper.SetDefault("pool.retry_delay", 100)

	// VAD Pool defaults
	viper.SetDefault("vad_pool.pool_size", 20)
	viper.SetDefault("vad_pool.max_idle", 10)
	viper.SetDefault("vad_pool.cleanup_interval", 300)

	// Rate Limit defaults
	viper.SetDefault("rate_limit.requests_per_second", 100)
	viper.SetDefault("rate_limit.burst_size", 200)
	viper.SetDefault("rate_limit.max_connections", 50)

	// Response defaults
	viper.SetDefault("response.send_mode", "queue")
	viper.SetDefault("response.queue_buffer_size", 100)
	viper.SetDefault("response.fallback_to_sync", true)
	viper.SetDefault("response.max_retry_count", 3)
	viper.SetDefault("response.timeout_ms", 5000)

	// Speaker defaults
	viper.SetDefault("speaker.enabled", false)
	viper.SetDefault("speaker.model_path", "models/3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx")
	viper.SetDefault("speaker.num_threads", 4)
	viper.SetDefault("speaker.provider", "cpu")
	viper.SetDefault("speaker.threshold", 0.6)
	viper.SetDefault("speaker.data_dir", "data/speaker")

	// Logging defaults
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "text")
	viper.SetDefault("logging.output", "both")
	viper.SetDefault("logging.file_path", "logs/vad_asr_server.log")
	viper.SetDefault("logging.max_size", 100)
	viper.SetDefault("logging.max_backups", 5)
	viper.SetDefault("logging.max_age", 30)
	viper.SetDefault("logging.compress", true)
}

// LoadConfig 加载配置文件（保持向后兼容）
func LoadConfig(filename string) error {
	return InitConfig(filename)
}

// GetConfig 获取配置
func GetConfig() *Config {
	return &GlobalConfig
}

// GetViper 获取viper实例
func GetViper() *viper.Viper {
	return viper.GetViper()
}

// WatchConfig 监听配置文件变化
func WatchConfig(callback func()) {
	viper.WatchConfig()

	// 添加防抖动计时器
	var debounceTimer *time.Timer
	viper.OnConfigChange(func(e fsnotify.Event) {
		// 如果已经有计时器在运行，重置它
		if debounceTimer != nil {
			debounceTimer.Stop()
		}

		// 设置新的计时器，延迟1秒执行
		debounceTimer = time.AfterFunc(time.Second, func() {
			fmt.Printf("🔄 Config file changed: %s\n", e.Name)
			// 重新解析配置
			if err := viper.Unmarshal(&GlobalConfig); err != nil {
				fmt.Printf("❌ Error reloading config: %v\n", err)
				return
			}
			if callback != nil {
				callback()
			}
		})
	})
}

// SaveConfig 保存配置到文件
func SaveConfig() error {
	return viper.WriteConfig()
}

// SaveConfigAs 保存配置到指定文件
func SaveConfigAs(filename string) error {
	return viper.WriteConfigAs(filename)
}

// SetConfigValue 设置配置值
func SetConfigValue(key string, value interface{}) {
	viper.Set(key, value)
	// 重新解析到结构体
	viper.Unmarshal(&GlobalConfig)
}

// GetConfigValue 获取配置值
func GetConfigValue(key string) interface{} {
	return viper.Get(key)
}

// GetString 获取字符串配置值
func GetString(key string) string {
	return viper.GetString(key)
}

// GetInt 获取整数配置值
func GetInt(key string) int {
	return viper.GetInt(key)
}

// GetBool 获取布尔配置值
func GetBool(key string) bool {
	return viper.GetBool(key)
}

// GetFloat64 获取浮点数配置值
func GetFloat64(key string) float64 {
	return viper.GetFloat64(key)
}

// PrintConfig 打印当前配置
func PrintConfig() {
	fmt.Println("📋 Current Configuration:")
	fmt.Printf("  Server: %s:%d\n", GlobalConfig.Server.Host, GlobalConfig.Server.Port)
	fmt.Printf("  VAD Model: %s\n", GlobalConfig.VAD.ModelPath)
	fmt.Printf("  ASR Model: %s\n", GlobalConfig.Recognition.ModelPath)
	fmt.Printf("  Pool Workers: %d\n", GlobalConfig.Pool.WorkerCount)
	fmt.Printf("  VAD Pool Size: %d\n", GlobalConfig.VADPool.PoolSize)
	fmt.Printf("  Log Level: %s\n", GlobalConfig.Logging.Level)
	fmt.Printf("  Log Output: %s\n", GlobalConfig.Logging.Output)
}
