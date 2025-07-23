package speaker

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"asr_server/internal/logger"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// SpeakerData 声纹数据结构
type SpeakerData struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Embeddings  [][]float32 `json:"embeddings"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	SampleCount int         `json:"sample_count"`
}

// SpeakerDatabase 声纹数据库结构
type SpeakerDatabase struct {
	Speakers  map[string]*SpeakerData `json:"speakers"`
	Version   string                  `json:"version"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// Manager 声纹识别管理器
type Manager struct {
	extractor    *sherpa.SpeakerEmbeddingExtractor
	manager      *sherpa.SpeakerEmbeddingManager
	database     *SpeakerDatabase
	dbPath       string
	threshold    float32
	embeddingDim int
	mutex        sync.RWMutex
	dataDir      string
}

// Config 声纹识别配置
type Config struct {
	ModelPath  string  `json:"model_path"`
	NumThreads int     `json:"num_threads"`
	Provider   string  `json:"provider"`
	Threshold  float32 `json:"threshold"`
	DataDir    string  `json:"data_dir"`
}

// NewManager 创建声纹识别管理器
func NewManager(config *Config) (*Manager, error) {
	// 确保数据目录存在
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	// 创建声纹特征提取器配置
	extractorConfig := &sherpa.SpeakerEmbeddingExtractorConfig{
		Model:      config.ModelPath,
		NumThreads: config.NumThreads,
		Debug:      0,
		Provider:   config.Provider,
	}

	// 创建声纹特征提取器
	extractor := sherpa.NewSpeakerEmbeddingExtractor(extractorConfig)
	if extractor == nil {
		return nil, fmt.Errorf("failed to create speaker embedding extractor")
	}

	// 获取特征维度
	dim := extractor.Dim()
	logger.Infof("Speaker embedding dimension: %d", dim)

	// 创建声纹管理器
	embeddingManager := sherpa.NewSpeakerEmbeddingManager(dim)
	if embeddingManager == nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(extractor)
		return nil, fmt.Errorf("failed to create speaker embedding manager")
	}

	manager := &Manager{
		extractor:    extractor,
		manager:      embeddingManager,
		threshold:    config.Threshold,
		embeddingDim: dim,
		dataDir:      config.DataDir,
		dbPath:       filepath.Join(config.DataDir, "speaker.json"),
	}

	// 加载现有数据库
	if err := manager.loadDatabase(); err != nil {
		logger.Infof("Warning: failed to load existing database: %v", err)
		manager.database = &SpeakerDatabase{
			Speakers:  make(map[string]*SpeakerData),
			Version:   "1.0.0",
			UpdatedAt: time.Now(),
		}
	}

	// 将数据库中的声纹加载到内存管理器
	if err := manager.loadSpeakersToMemory(); err != nil {
		logger.Infof("Warning: failed to load speakers to memory: %v", err)
	}

	return manager, nil
}

// Close 关闭管理器并释放资源
func (m *Manager) Close() {
	if m.extractor != nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(m.extractor)
	}
	if m.manager != nil {
		sherpa.DeleteSpeakerEmbeddingManager(m.manager)
	}
}

// loadDatabase 从文件加载声纹数据库
func (m *Manager) loadDatabase() error {
	if _, err := os.Stat(m.dbPath); os.IsNotExist(err) {
		return fmt.Errorf("database file does not exist")
	}

	data, err := ioutil.ReadFile(m.dbPath)
	if err != nil {
		return fmt.Errorf("failed to read database file: %v", err)
	}

	var db SpeakerDatabase
	if err := json.Unmarshal(data, &db); err != nil {
		return fmt.Errorf("failed to unmarshal database: %v", err)
	}

	m.database = &db
	return nil
}

// saveDatabase 保存声纹数据库到文件
func (m *Manager) saveDatabase() error {
	m.database.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(m.database, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal database: %v", err)
	}

	if err := ioutil.WriteFile(m.dbPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write database file: %v", err)
	}

	return nil
}

// loadSpeakersToMemory 将数据库中的声纹加载到内存管理器
func (m *Manager) loadSpeakersToMemory() error {
	loadedCount := 0
	totalEmbeddings := 0

	for speakerID, speakerData := range m.database.Speakers {
		if len(speakerData.Embeddings) > 0 {
			// 注册多个嵌入向量
			success := m.manager.RegisterV(speakerID, speakerData.Embeddings)
			if !success {
				logger.Infof("Warning: failed to register speaker %s to memory", speakerID)
			} else {
				loadedCount++
				totalEmbeddings += len(speakerData.Embeddings)
			}
		}
	}

	logger.Infof("✅ Loaded %d speakers with %d total embeddings to memory for fast recognition",
		loadedCount, totalEmbeddings)
	return nil
}

// extractEmbedding 从音频数据提取声纹特征
func (m *Manager) extractEmbedding(audioData []float32, sampleRate int) ([]float32, error) {
	// 创建音频流
	stream := m.extractor.CreateStream()
	defer sherpa.DeleteOnlineStream(stream)

	// 接受音频数据
	stream.AcceptWaveform(sampleRate, audioData)
	stream.InputFinished()

	// 检查是否准备就绪
	if !m.extractor.IsReady(stream) {
		return nil, fmt.Errorf("insufficient audio data for embedding extraction")
	}

	// 提取特征
	embedding := m.extractor.Compute(stream)
	if len(embedding) == 0 {
		return nil, fmt.Errorf("failed to extract embedding")
	}

	return embedding, nil
}

// calculateSimilarity 计算声纹特征向量的相似度
func (m *Manager) calculateSimilarity(queryEmbedding []float32, storedEmbeddings [][]float32) float32 {
	if len(storedEmbeddings) == 0 {
		return 0.0
	}

	maxSimilarity := float32(0.0)

	// 与所有存储的特征向量计算余弦相似度，取最大值
	for _, embedding := range storedEmbeddings {
		similarity := cosineSimilarity(queryEmbedding, embedding)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
		}
	}

	return maxSimilarity
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float32

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	similarity := dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
	return similarity
}

// RegisterSpeaker 注册声纹
func (m *Manager) RegisterSpeaker(speakerID, speakerName string, audioData []float32, sampleRate int) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 提取声纹特征
	embedding, err := m.extractEmbedding(audioData, sampleRate)
	if err != nil {
		return fmt.Errorf("failed to extract embedding: %v", err)
	}

	// 检查说话人是否已存在
	speakerData, exists := m.database.Speakers[speakerID]
	if !exists {
		// 创建新的说话人数据
		speakerData = &SpeakerData{
			ID:          speakerID,
			Name:        speakerName,
			Embeddings:  [][]float32{},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			SampleCount: 0,
		}
		m.database.Speakers[speakerID] = speakerData
	}

	// 添加新的嵌入向量
	speakerData.Embeddings = append(speakerData.Embeddings, embedding)
	speakerData.UpdatedAt = time.Now()
	speakerData.SampleCount++
	speakerData.Name = speakerName // 更新名称

	// 注册到内存管理器
	success := m.manager.RegisterV(speakerID, speakerData.Embeddings)
	if !success {
		return fmt.Errorf("failed to register speaker to memory manager")
	}

	// 保存到文件
	if err := m.saveDatabase(); err != nil {
		return fmt.Errorf("failed to save database: %v", err)
	}

	logger.Infof("Successfully registered speaker %s (%s) with %d samples",
		speakerID, speakerName, speakerData.SampleCount)
	return nil
}

// IdentifySpeaker 识别声纹（直接使用内存中的数据进行高效对比）
func (m *Manager) IdentifySpeaker(audioData []float32, sampleRate int) (*IdentifyResult, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 提取声纹特征
	embedding, err := m.extractEmbedding(audioData, sampleRate)
	if err != nil {
		return nil, fmt.Errorf("failed to extract embedding: %v", err)
	}

	// 在内存管理器中搜索最佳匹配（已加载的声纹数据直接内存对比）
	speakerID := m.manager.Search(embedding, m.threshold)

	result := &IdentifyResult{
		Identified:  false,
		SpeakerID:   "",
		SpeakerName: "",
		Confidence:  0.0,
		Threshold:   m.threshold,
	}

	if speakerID != "" {
		// 找到匹配的说话人
		speakerData, exists := m.database.Speakers[speakerID]
		if exists {
			result.Identified = true
			result.SpeakerID = speakerID
			result.SpeakerName = speakerData.Name

			// 计算精确的相似度分数
			confidence := m.calculateSimilarity(embedding, speakerData.Embeddings)
			result.Confidence = confidence
		}
	}

	return result, nil
}

// VerifySpeaker 验证声纹（直接使用内存中的数据进行高效对比）
func (m *Manager) VerifySpeaker(speakerID string, audioData []float32, sampleRate int) (*VerifyResult, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 检查说话人是否存在
	speakerData, exists := m.database.Speakers[speakerID]
	if !exists {
		return nil, fmt.Errorf("speaker %s not found", speakerID)
	}

	// 提取声纹特征
	embedding, err := m.extractEmbedding(audioData, sampleRate)
	if err != nil {
		return nil, fmt.Errorf("failed to extract embedding: %v", err)
	}

	// 计算精确的相似度分数
	confidence := m.calculateSimilarity(embedding, speakerData.Embeddings)
	verified := confidence >= m.threshold

	result := &VerifyResult{
		SpeakerID:   speakerID,
		SpeakerName: speakerData.Name,
		Verified:    verified,
		Confidence:  confidence,
		Threshold:   m.threshold,
	}

	return result, nil
}

// GetAllSpeakers 获取所有注册的说话人
func (m *Manager) GetAllSpeakers() []*SpeakerInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	speakers := make([]*SpeakerInfo, 0, len(m.database.Speakers))
	for _, speakerData := range m.database.Speakers {
		speakers = append(speakers, &SpeakerInfo{
			ID:          speakerData.ID,
			Name:        speakerData.Name,
			SampleCount: speakerData.SampleCount,
			CreatedAt:   speakerData.CreatedAt,
			UpdatedAt:   speakerData.UpdatedAt,
		})
	}

	return speakers
}

// DeleteSpeaker 删除说话人
func (m *Manager) DeleteSpeaker(speakerID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查说话人是否存在
	if _, exists := m.database.Speakers[speakerID]; !exists {
		return fmt.Errorf("speaker %s not found", speakerID)
	}

	// 从数据库删除
	delete(m.database.Speakers, speakerID)

	// 从内存管理器删除
	m.manager.Remove(speakerID)

	// 保存到文件
	if err := m.saveDatabase(); err != nil {
		return fmt.Errorf("failed to save database: %v", err)
	}

	logger.Infof("Successfully deleted speaker %s", speakerID)
	return nil
}

// GetStats 获取统计信息（用于主服务监控）
func (m *Manager) GetStats() map[string]interface{} {
	stats := m.GetDatabaseStats()
	return map[string]interface{}{
		"speaker_count": stats.TotalSpeakers,
		"total_samples": stats.TotalSamples,
		"embedding_dim": stats.EmbeddingDim,
		"threshold":     stats.Threshold,
		"version":       stats.Version,
		"last_updated":  stats.UpdatedAt.Format(time.RFC3339),
	}
}

// GetDatabaseStats 获取数据库统计信息
func (m *Manager) GetDatabaseStats() *DatabaseStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalSamples := 0
	for _, speaker := range m.database.Speakers {
		totalSamples += speaker.SampleCount
	}

	return &DatabaseStats{
		TotalSpeakers: len(m.database.Speakers),
		TotalSamples:  totalSamples,
		EmbeddingDim:  m.embeddingDim,
		Threshold:     m.threshold,
		Version:       m.database.Version,
		UpdatedAt:     m.database.UpdatedAt,
	}
}

// 响应结构体定义
type IdentifyResult struct {
	Identified  bool    `json:"identified"`
	SpeakerID   string  `json:"speaker_id"`
	SpeakerName string  `json:"speaker_name"`
	Confidence  float32 `json:"confidence"`
	Threshold   float32 `json:"threshold"`
}

type VerifyResult struct {
	SpeakerID   string  `json:"speaker_id"`
	SpeakerName string  `json:"speaker_name"`
	Verified    bool    `json:"verified"`
	Confidence  float32 `json:"confidence"`
	Threshold   float32 `json:"threshold"`
}

type SpeakerInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SampleCount int       `json:"sample_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DatabaseStats struct {
	TotalSpeakers int       `json:"total_speakers"`
	TotalSamples  int       `json:"total_samples"`
	EmbeddingDim  int       `json:"embedding_dim"`
	Threshold     float32   `json:"threshold"`
	Version       string    `json:"version"`
	UpdatedAt     time.Time `json:"updated_at"`
}
