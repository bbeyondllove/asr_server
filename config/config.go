package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 配置结构
type Config struct {
	Server struct {
		Port           int    `mapstructure:"port"`
		Host           string `mapstructure:"host"`
		MaxConnections int    `mapstructure:"max_connections"`
		ReadTimeout    int    `mapstructure:"read_timeout"`
		WebSocket      struct {
			ReadTimeout       int  `mapstructure:"read_timeout"`
			MaxMessageSize    int  `mapstructure:"max_message_size"`
			ReadBufferSize    int  `mapstructure:"read_buffer_size"`
			WriteBufferSize   int  `mapstructure:"write_buffer_size"`
			EnableCompression bool `mapstructure:"enable_compression"`
		} `mapstructure:"websocket"`
	} `mapstructure:"server"`
	Session struct {
		SendQueueSize int `mapstructure:"send_queue_size"`
		MaxSendErrors int `mapstructure:"max_send_errors"`
	} `mapstructure:"session"`
	VAD         VADConfig `mapstructure:"vad"`
	Recognition struct {
		ModelPath                   string `mapstructure:"model_path"`
		TokensPath                  string `mapstructure:"tokens_path"`
		Language                    string `mapstructure:"language"`
		UseInverseTextNormalization bool   `mapstructure:"use_inverse_text_normalization"`
		NumThreads                  int    `mapstructure:"num_threads"`
		Provider                    string `mapstructure:"provider"`
		Debug                       bool   `mapstructure:"debug"`
	} `mapstructure:"recognition"`
	Speaker struct {
		Enabled    bool    `mapstructure:"enabled"`
		ModelPath  string  `mapstructure:"model_path"`
		NumThreads int     `mapstructure:"num_threads"`
		Provider   string  `mapstructure:"provider"`
		Threshold  float32 `mapstructure:"threshold"`
		DataDir    string  `mapstructure:"data_dir"`
	} `mapstructure:"speaker"`
	Audio struct {
		SampleRate      int     `mapstructure:"sample_rate"`
		FeatureDim      int     `mapstructure:"feature_dim"`
		NormalizeFactor float32 `mapstructure:"normalize_factor"`
		ChunkSize       int     `mapstructure:"chunk_size"`
	} `mapstructure:"audio"`
	Pool struct {
		InstanceMode string `mapstructure:"instance_mode"`
		WorkerCount  int    `mapstructure:"worker_count"`
		QueueSize    int    `mapstructure:"queue_size"`
	} `mapstructure:"pool"`
	RateLimit struct {
		Enabled           bool `mapstructure:"enabled"`
		RequestsPerSecond int  `mapstructure:"requests_per_second"`
		BurstSize         int  `mapstructure:"burst_size"`
		MaxConnections    int  `mapstructure:"max_connections"`
	} `mapstructure:"rate_limit"`
	Response struct {
		SendMode string `mapstructure:"send_mode"`
		Timeout  int    `mapstructure:"timeout"`
	} `mapstructure:"response"`
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

type VADConfig struct {
	Provider  string        `mapstructure:"provider"`
	PoolSize  int           `mapstructure:"pool_size"`
	Threshold float32       `mapstructure:"threshold"`
	SileroVAD SileroVADConf `mapstructure:"silero_vad"`
	TenVAD    TenVADConf    `mapstructure:"ten_vad"`
}

type SileroVADConf struct {
	ModelPath          string  `mapstructure:"model_path"`
	Threshold          float32 `mapstructure:"threshold"`
	MinSilenceDuration float32 `mapstructure:"min_silence_duration"`
	MinSpeechDuration  float32 `mapstructure:"min_speech_duration"`
	MaxSpeechDuration  float32 `mapstructure:"max_speech_duration"`
	WindowSize         int     `mapstructure:"window_size"`
	BufferSizeSeconds  float32 `mapstructure:"buffer_size_seconds"`
}

type TenVADConf struct {
	HopSize          int `mapstructure:"hop_size"`
	MinSpeechFrames  int `mapstructure:"min_speech_frames"`
	MaxSilenceFrames int `mapstructure:"max_silence_frames"`
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

// WatchConfig 监听配置文件变化 (已废弃，使用HotReloadManager)
func WatchConfig(callback func()) {
	fmt.Println("⚠️  WatchConfig is deprecated, use HotReloadManager instead")
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
	fmt.Printf("  VAD Model: %s\n", GlobalConfig.VAD.SileroVAD.ModelPath)
	fmt.Printf("  ASR Model: %s\n", GlobalConfig.Recognition.ModelPath)
	fmt.Printf("  Pool Workers: %d\n", GlobalConfig.Pool.WorkerCount)
	fmt.Printf("  VAD Pool Size: %d\n", GlobalConfig.VAD.PoolSize)
	fmt.Printf("  Log Level: %s\n", GlobalConfig.Logging.Level)
	fmt.Printf("  Log Output: %s\n", GlobalConfig.Logging.FilePath)
}
