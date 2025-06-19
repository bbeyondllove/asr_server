package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"asr_server/config"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/sirupsen/logrus"
)

// StreamPool 使用真正的Stream池进行复用
type StreamPool struct {
	recognizer  *sherpa.OfflineRecognizer
	streamPool  chan *sherpa.OfflineStream // Stream池
	poolSize    int                        // 池大小
	config      *config.Config
	stats       *PoolStats
	taskQueue   chan *Task
	workerCount int
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *logrus.Logger
	wg          sync.WaitGroup
	mu          sync.RWMutex // 保护池状态
}

// NewStreamPool 创建基于Stream池的资源池
func NewStreamPool(cfg *config.Config, logger *logrus.Logger) (*StreamPool, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建全局唯一的recognizer实例
	recognizer, err := createRecognizer(cfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create recognizer: %w", err)
	}

	// 计算Stream池大小，通常为worker数量的2倍以确保充足的资源
	poolSize := cfg.Pool.WorkerCount * 2
	if poolSize < 4 {
		poolSize = 4 // 最小池大小
	}

	pool := &StreamPool{
		recognizer:  recognizer,
		streamPool:  make(chan *sherpa.OfflineStream, poolSize),
		poolSize:    poolSize,
		config:      cfg,
		stats:       NewPoolStats(),
		taskQueue:   make(chan *Task, cfg.Pool.QueueSize),
		workerCount: cfg.Pool.WorkerCount,
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
	}

	// 预先创建Stream填充池
	for i := 0; i < poolSize; i++ {
		stream := sherpa.NewOfflineStream(recognizer)
		if stream == nil {
			// 如果创建失败，清理已创建的streams
			pool.closeAllStreams()
			cancel()
			return nil, fmt.Errorf("failed to create stream %d for pool", i)
		}
		pool.streamPool <- stream
	}

	// 启动worker协程
	for i := 0; i < cfg.Pool.WorkerCount; i++ {
		pool.wg.Add(1)
		go pool.runWorker(i)
	}

	logger.WithFields(logrus.Fields{
		"workers":          cfg.Pool.WorkerCount,
		"queue_size":       cfg.Pool.QueueSize,
		"stream_pool_size": poolSize,
		"architecture":     "single-recognizer-stream-pool",
	}).Info("Stream资源池启动成功")

	return pool, nil
}

// getStream 从池中获取一个Stream
func (p *StreamPool) getStream() (*sherpa.OfflineStream, error) {
	select {
	case stream := <-p.streamPool:
		return stream, nil
	case <-p.ctx.Done():
		return nil, fmt.Errorf("pool is shutting down")
	default:
		// 池为空，创建临时Stream（这种情况不应该经常发生）
		p.logger.Warn("Stream池暂时为空，创建临时Stream")
		stream := sherpa.NewOfflineStream(p.recognizer)
		if stream == nil {
			return nil, fmt.Errorf("failed to create temporary stream")
		}
		return stream, nil
	}
}

// returnStream 将Stream归还给池
func (p *StreamPool) returnStream(stream *sherpa.OfflineStream) {
	if stream == nil {
		return
	}

	select {
	case p.streamPool <- stream:
		// 成功归还到池
	case <-p.ctx.Done():
		// 池正在关闭，直接释放Stream
		sherpa.DeleteOfflineStream(stream)
	default:
		// 池已满，释放这个Stream（临时创建的Stream）
		p.logger.Debug("Stream池已满，释放临时Stream")
		sherpa.DeleteOfflineStream(stream)
	}
}

// closeAllStreams 关闭池中所有Stream
func (p *StreamPool) closeAllStreams() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 清空并释放池中的所有Stream
	for {
		select {
		case stream := <-p.streamPool:
			if stream != nil {
				sherpa.DeleteOfflineStream(stream)
			}
		default:
			return // 池已空
		}
	}
}

func (p *StreamPool) runWorker(workerID int) {
	defer p.wg.Done()

	workerIDStr := fmt.Sprintf("worker-%d", workerID)
	p.logger.WithField("worker_id", workerIDStr).Debug("Worker启动")

	for {
		select {
		case <-p.ctx.Done():
			p.logger.WithField("worker_id", workerIDStr).Debug("Worker停止")
			return
		case task := <-p.taskQueue:
			if task == nil {
				return
			}
			p.processTask(workerIDStr, task)
		}
	}
}

func (p *StreamPool) processTask(workerID string, task *Task) {
	startTime := time.Now()

	defer func() {
		if r := recover(); r != nil {
			p.logger.WithFields(logrus.Fields{
				"worker_id":   workerID,
				"session_id":  task.SessionID,
				"error":       r,
				"sample_rate": task.SampleRate,
				"samples_len": len(task.Samples),
			}).Error("Worker发生严重错误 - sherpa-onnx库可能崩溃")

			// 发送错误响应
			if task.ResultChan != nil {
				task.ResultChan <- &Result{
					Error: fmt.Errorf("ASR处理器发生内部错误: %v", r),
				}
				close(task.ResultChan)
			}

			// 尝试回调通知
			if task.Callback != nil {
				task.Callback("", fmt.Errorf("ASR处理器内部错误: %v", r))
			}
		}

		// 更新统计
		duration := time.Since(startTime)
		atomic.AddInt64(&p.stats.TasksProcessed, 1)
		atomic.AddInt64(&p.stats.TotalProcessingTime, int64(duration))
		if duration > time.Duration(atomic.LoadInt64(&p.stats.MaxProcessingTime)) {
			atomic.StoreInt64(&p.stats.MaxProcessingTime, int64(duration))
		}
	}()

	// 验证音频数据
	if len(task.Samples) == 0 {
		err := fmt.Errorf("音频数据为空")
		p.logger.WithFields(logrus.Fields{
			"worker_id":  workerID,
			"session_id": task.SessionID,
		}).Warn(err.Error())

		if task.ResultChan != nil {
			task.ResultChan <- &Result{Error: err}
			close(task.ResultChan)
		}
		if task.Callback != nil {
			task.Callback("", err)
		}
		return
	}

	if task.SampleRate <= 0 {
		err := fmt.Errorf("无效的采样率: %d", task.SampleRate)
		p.logger.WithFields(logrus.Fields{
			"worker_id":   workerID,
			"session_id":  task.SessionID,
			"sample_rate": task.SampleRate,
		}).Warn(err.Error())

		if task.ResultChan != nil {
			task.ResultChan <- &Result{Error: err}
			close(task.ResultChan)
		}
		if task.Callback != nil {
			task.Callback("", err)
		}
		return
	}

	p.logger.WithFields(logrus.Fields{
		"worker_id":   workerID,
		"session_id":  task.SessionID,
		"samples_len": len(task.Samples),
		"sample_rate": task.SampleRate,
		"duration_ms": int(float64(len(task.Samples)) / float64(task.SampleRate) * 1000),
	}).Debug("开始处理音频任务")

	// ===== 使用Stream池进行音频识别 =====
	text := p.decodeAudio(workerID, task.SessionID, task.SampleRate, task.Samples)

	// 成功处理
	p.logger.WithFields(logrus.Fields{
		"worker_id":   workerID,
		"session_id":  task.SessionID,
		"result":      text,
		"duration_ms": time.Since(startTime).Milliseconds(),
	}).Info("识别完成")

	if task.ResultChan != nil {
		task.ResultChan <- &Result{Text: text}
		close(task.ResultChan)
	}
	if task.Callback != nil {
		task.Callback(text, nil)
	}
}

// decodeAudio 使用Stream池进行音频识别
func (p *StreamPool) decodeAudio(workerID, sessionID string, sampleRate int, samples []float32) string {
	p.logger.WithFields(logrus.Fields{
		"worker_id":  workerID,
		"session_id": sessionID,
	}).Debug("从池中获取Stream进行音频识别")

	// 从池中获取Stream
	stream, err := p.getStream()
	if err != nil {
		p.logger.WithFields(logrus.Fields{
			"worker_id":  workerID,
			"session_id": sessionID,
			"error":      err,
		}).Error("无法从池中获取Stream")
		return ""
	}

	// 确保Stream会被归还到池
	defer p.returnStream(stream)

	// 处理音频数据
	stream.AcceptWaveform(sampleRate, samples)
	p.recognizer.Decode(stream)
	result := stream.GetResult()

	if result == nil {
		p.logger.WithFields(logrus.Fields{
			"worker_id":  workerID,
			"session_id": sessionID,
		}).Warn("识别结果为空")
		return ""
	}

	text := result.Text
	// 可选的文本处理（如go-sherpa-server中的toLowerCase和trim）
	// text = strings.ToLower(text)
	// text = strings.TrimSpace(text)

	return text
}

// SubmitTask 提交任务到stream资源池
func (p *StreamPool) SubmitTask(task *Task) error {
	select {
	case p.taskQueue <- task:
		atomic.AddInt64(&p.stats.TasksSubmitted, 1)
		return nil
	case <-p.ctx.Done():
		return ErrPoolShutdown
	default:
		atomic.AddInt64(&p.stats.TasksRejected, 1)
		return ErrQueueFull
	}
}

// GetStats 获取统计信息 - 返回map格式兼容现有代码
func (p *StreamPool) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"tasks_submitted":       atomic.LoadInt64(&p.stats.TasksSubmitted),
		"tasks_processed":       atomic.LoadInt64(&p.stats.TasksProcessed),
		"tasks_rejected":        atomic.LoadInt64(&p.stats.TasksRejected),
		"total_processing_time": atomic.LoadInt64(&p.stats.TotalProcessingTime),
		"max_processing_time":   atomic.LoadInt64(&p.stats.MaxProcessingTime),
		"worker_count":          p.workerCount,
		"queue_size":            cap(p.taskQueue),
		"current_queue_length":  len(p.taskQueue),
		"stream_pool_size":      p.poolSize,
		"available_streams":     len(p.streamPool),
	}
	return stats
}

// Shutdown 关闭资源池 - 兼容现有接口
func (p *StreamPool) Shutdown() {
	p.Close()
}

// Close 关闭资源池
func (p *StreamPool) Close() error {
	p.cancel()
	close(p.taskQueue)

	p.wg.Wait()

	// 关闭所有Stream
	p.closeAllStreams()

	if p.recognizer != nil {
		sherpa.DeleteOfflineRecognizer(p.recognizer)
	}

	p.logger.Info("Stream资源池已关闭")
	return nil
}

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
