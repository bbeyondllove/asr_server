package speaker

import (
	"asr_server/config"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-audio/wav"
)

// Handler 声纹识别HTTP处理器
type Handler struct {
	manager *Manager
}

// NewHandler 创建新的处理器
func NewHandler(manager *Manager) *Handler {
	return &Handler{
		manager: manager,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	speakerGroup := router.Group("/api/v1/speaker")
	{
		// 声纹注册
		speakerGroup.POST("/register", h.RegisterSpeaker)

		// 声纹识别
		speakerGroup.POST("/identify", h.IdentifySpeaker)

		// 声纹验证
		speakerGroup.POST("/verify/:speaker_id", h.VerifySpeaker)

		// 获取所有说话人
		speakerGroup.GET("/list", h.GetAllSpeakers)

		// 删除说话人
		speakerGroup.DELETE("/:speaker_id", h.DeleteSpeaker)

		// 获取数据库统计信息
		speakerGroup.GET("/stats", h.GetStats)

		//Base64 注册与识别接口
		speakerGroup.POST("/register_base64", h.RegisterSpeakerBase64)
		speakerGroup.POST("/identify_base64", h.IdentifySpeakerBase64)
	}
}

// RegisterSpeaker 注册声纹
func (h *Handler) RegisterSpeaker(c *gin.Context) {
	// 获取表单数据
	speakerID := c.PostForm("speaker_id")
	speakerName := c.PostForm("speaker_name")

	if speakerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "speaker_id is required",
		})
		return
	}

	if speakerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "speaker_name is required",
		})
		return
	}

	// 获取音频文件
	file, header, err := c.Request.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "audio file is required",
		})
		return
	}
	defer file.Close()

	// 解析音频数据
	audioData, sampleRate, err := h.parseAudioFile(file, header)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to parse audio file: %v", err),
		})
		return
	}

	// 注册声纹
	err = h.manager.RegisterSpeaker(speakerID, speakerName, audioData, sampleRate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to register speaker: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Speaker registered successfully",
		"speaker_id":   speakerID,
		"speaker_name": speakerName,
	})
}

// IdentifySpeaker 识别声纹
func (h *Handler) IdentifySpeaker(c *gin.Context) {
	// 获取音频文件
	file, header, err := c.Request.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "audio file is required",
		})
		return
	}
	defer file.Close()

	// 解析音频数据
	audioData, sampleRate, err := h.parseAudioFile(file, header)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to parse audio file: %v", err),
		})
		return
	}

	// 识别声纹
	result, err := h.manager.IdentifySpeaker(audioData, sampleRate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to identify speaker: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// VerifySpeaker 验证声纹
func (h *Handler) VerifySpeaker(c *gin.Context) {
	speakerID := c.Param("speaker_id")
	if speakerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "speaker_id is required",
		})
		return
	}

	// 获取音频文件
	file, header, err := c.Request.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "audio file is required",
		})
		return
	}
	defer file.Close()

	// 解析音频数据
	audioData, sampleRate, err := h.parseAudioFile(file, header)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("failed to parse audio file: %v", err),
		})
		return
	}

	// 验证声纹
	result, err := h.manager.VerifySpeaker(speakerID, audioData, sampleRate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to verify speaker: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetAllSpeakers 获取所有说话人
func (h *Handler) GetAllSpeakers(c *gin.Context) {
	speakers := h.manager.GetAllSpeakers()
	c.JSON(http.StatusOK, gin.H{
		"speakers": speakers,
		"total":    len(speakers),
	})
}

// DeleteSpeaker 删除说话人
func (h *Handler) DeleteSpeaker(c *gin.Context) {
	speakerID := c.Param("speaker_id")
	if speakerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "speaker_id is required",
		})
		return
	}

	err := h.manager.DeleteSpeaker(speakerID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to delete speaker: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Speaker deleted successfully",
		"speaker_id": speakerID,
	})
}

// GetStats 获取数据库统计信息
func (h *Handler) GetStats(c *gin.Context) {
	stats := h.manager.GetDatabaseStats()
	c.JSON(http.StatusOK, stats)
}

// parseAudioFile 解析音频文件
func (h *Handler) parseAudioFile(file multipart.File, header *multipart.FileHeader) ([]float32, int, error) {
	// 检查文件类型
	filename := strings.ToLower(header.Filename)
	if !strings.HasSuffix(filename, ".wav") {
		return nil, 0, fmt.Errorf("only WAV files are supported")
	}

	// 读取WAV文件
	decoder := wav.NewDecoder(file)
	if !decoder.IsValidFile() {
		return nil, 0, fmt.Errorf("invalid WAV file")
	}

	// 获取音频格式信息
	sampleRate := int(decoder.SampleRate)
	numChannels := int(decoder.NumChans)

	// 只支持单声道或立体声
	if numChannels > 2 {
		return nil, 0, fmt.Errorf("unsupported number of channels: %d", numChannels)
	}

	// 读取音频数据
	buffer, err := decoder.FullPCMBuffer()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode audio: %v", err)
	}

	// 转换为float32格式
	samples := make([]float32, len(buffer.Data))
	for i, sample := range buffer.Data {
		// 将int转换为float32，范围[-1.0, 1.0]
		samples[i] = float32(sample) / config.GlobalConfig.Audio.NormalizeFactor
	}

	// 如果是立体声，转换为单声道（取平均值）
	if numChannels == 2 {
		monoSamples := make([]float32, len(samples)/2)
		for i := 0; i < len(monoSamples); i++ {
			monoSamples[i] = (samples[i*2] + samples[i*2+1]) / 2.0
		}
		samples = monoSamples
	}

	return samples, sampleRate, nil
}

// 添加基于Base64的API接口（可选）

// RegisterSpeakerBase64 使用Base64编码的音频数据注册声纹
func (h *Handler) RegisterSpeakerBase64(c *gin.Context) {
	var req struct {
		SpeakerID   string `json:"speaker_id" binding:"required"`
		SpeakerName string `json:"speaker_name" binding:"required"`
		AudioData   string `json:"audio_data" binding:"required"` // Base64编码的WAV数据
		SampleRate  int    `json:"sample_rate" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 这里可以添加Base64解码和音频处理逻辑
	// 为简化示例，暂时跳过具体实现

	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "Base64 API not implemented yet",
	})
}

// IdentifySpeakerBase64 使用Base64编码的音频数据识别声纹
func (h *Handler) IdentifySpeakerBase64(c *gin.Context) {
	var req struct {
		AudioData  string `json:"audio_data" binding:"required"` // Base64编码的WAV数据
		SampleRate int    `json:"sample_rate" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 这里可以添加Base64解码和音频处理逻辑
	// 为简化示例，暂时跳过具体实现

	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "Base64 API not implemented yet",
	})
}
