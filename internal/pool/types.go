package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Pool 资源池接口 - 统一不同池实现的接口
type Pool interface {
	SubmitTask(task *Task) error
	GetStats() map[string]interface{}
	Shutdown()
}

// Task 任务结构 - 用于StreamPool
type Task struct {
	ID         string
	SessionID  string
	Samples    []float32
	SampleRate int
	ResultChan chan *Result
	Callback   func(string, error)
	Context    context.Context
	Timeout    time.Duration // 任务超时时间
	CreatedAt  time.Time     // 任务创建时间
}

// Result 识别结果
type Result struct {
	Text      string
	Timestamp time.Time
	Error     error
}

// PoolStats 池统计信息
type PoolStats struct {
	TasksSubmitted      int64
	TasksProcessed      int64
	TasksRejected       int64
	TotalProcessingTime int64 // 纳秒
	MaxProcessingTime   int64 // 纳秒
}

// NewPoolStats 创建新的统计实例
func NewPoolStats() *PoolStats {
	return &PoolStats{}
}

// Worker 工作器结构 - 保留原有的多实例架构
type Worker struct {
	ID         int
	recognizer *sherpa.OfflineRecognizer
	taskChan   chan *Task
	quit       chan bool
	wg         *sync.WaitGroup
	isActive   int32
}

// 错误定义
var (
	ErrPoolShutdown = fmt.Errorf("pool is shutdown")
	ErrQueueFull    = fmt.Errorf("task queue is full")
)

const (
	TEN_VAD_TYPE = "ten_vad"
	SILERO_TYPE  = "silero_vad"
)
