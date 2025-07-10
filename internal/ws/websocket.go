package ws

import (
	"asr_server/config"
	"asr_server/internal/logger"
	"asr_server/internal/session"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/gorilla/websocket"
)

// Upgrader 用于升级 WebSocket 连接
var Upgrader = websocket.Upgrader{
	CheckOrigin:       func(r *http.Request) bool { return true },
	ReadBufferSize:    config.GlobalConfig.Server.WebSocket.ReadBufferSize,
	WriteBufferSize:   config.GlobalConfig.Server.WebSocket.WriteBufferSize,
	EnableCompression: config.GlobalConfig.Server.WebSocket.EnableCompression,
}

// GenerateSessionID 生成会话ID
func GenerateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// HandleWebSocket 处理 WebSocket 连接
// 依赖注入 sessionManager, globalRecognizer
func HandleWebSocket(w http.ResponseWriter, r *http.Request, sessionManager *session.Manager, globalRecognizer *sherpa.OfflineRecognizer) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(fmt.Sprintf("WebSocket upgrade failed: %v", err))
		return
	}

	wsConfig := config.GlobalConfig.Server.WebSocket

	if wsConfig.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(time.Duration(wsConfig.ReadTimeout) * time.Second))
	}

	sessionID := GenerateSessionID()

	// 创建会话
	sess, err := sessionManager.CreateSession(sessionID, conn)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create session, session_id=%s, error=%v", sessionID, err))
		conn.Close()
		return
	}

	defer func() {
		sessionManager.RemoveSession(sessionID)
		logger.Info(fmt.Sprintf("WebSocket connection closed, session_id=%s", sessionID))
	}()

	logger.Info(fmt.Sprintf("New WebSocket connection established, session_id=%s", sessionID))

	// 发送连接确认
	if sess != nil {
		select {
		case sess.SendQueue <- map[string]interface{}{
			"type":       "connection",
			"message":    "WebSocket connected, ready for audio",
			"session_id": sessionID,
		}:
		default:
			logger.Warn("Session send queue is full, dropping connection confirmation")
		}
	}

	// 处理消息
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Warn("WebSocket read error")
			break
		}

		// 每次收到消息都刷新读超时
		if wsConfig.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(time.Duration(wsConfig.ReadTimeout) * time.Second))
		}

		// 检查消息大小
		if wsConfig.MaxMessageSize > 0 && len(message) > wsConfig.MaxMessageSize {
			logger.Warn("Message too large, closing connection")
			break
		}

		// 处理音频数据
		if len(message) > 0 {
			if err := sessionManager.ProcessAudioData(sessionID, message); err != nil {
				logger.Error(fmt.Sprintf("Failed to process audio data, session_id=%s, error=%v", sessionID, err))
				// 通过session的SendQueue发送错误消息
				if sess != nil {
					select {
					case sess.SendQueue <- map[string]interface{}{
						"type":    "error",
						"message": err.Error(),
					}:
					default:
						logger.Warn("Session send queue is full, dropping error message")
					}
				}
			}
		}
	}
}
