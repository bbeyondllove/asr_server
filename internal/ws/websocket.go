package ws

import (
	"asr_server/config"
	"asr_server/internal/logger"
	"asr_server/internal/pool"
	"asr_server/internal/session"
	"crypto/rand"
	"encoding/hex"
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
// 依赖注入 sessionManager, globalRecognizer, vadPool
func HandleWebSocket(w http.ResponseWriter, r *http.Request, sessionManager *session.Manager, globalRecognizer *sherpa.OfflineRecognizer, vadPool *pool.VADPool) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.WithError(err).Error("WebSocket upgrade failed")
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
		logger.WithFields(logger.Fields{
			"session_id": sessionID,
			"error":      err,
		}).Error("Failed to create session")
		conn.Close()
		return
	}

	defer func() {
		sessionManager.RemoveSession(sessionID)
		logger.WithField("session_id", sessionID).Info("WebSocket connection closed")
	}()

	logger.WithField("session_id", sessionID).Info("New WebSocket connection established")

	// 发送连接确认
	if sess != nil {
		select {
		case sess.SendQueue <- map[string]interface{}{
			"type":       "connection",
			"message":    "WebSocket connected, ready for audio",
			"session_id": sessionID,
		}:
		default:
			logger.Warnf("Session %s send queue is full, dropping connection confirmation", sessionID)
		}
	}

	// 处理消息
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.WithFields(logger.Fields{
				"session_id": sessionID,
				"error":      err,
			}).Warn("WebSocket read error")
			break
		}

		// 每次收到消息都刷新读超时
		if wsConfig.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(time.Duration(wsConfig.ReadTimeout) * time.Second))
		}

		// 检查消息大小
		if wsConfig.MaxMessageSize > 0 && len(message) > wsConfig.MaxMessageSize {
			logger.WithFields(logger.Fields{
				"session_id":   sessionID,
				"message_size": len(message),
				"max_size":     wsConfig.MaxMessageSize,
			}).Warn("Message too large, closing connection")
			break
		}

		// 处理音频数据
		if len(message) > 0 {
			if err := sessionManager.ProcessAudioData(sessionID, message); err != nil {
				logger.WithFields(logger.Fields{
					"session_id": sessionID,
					"error":      err,
				}).Error("Failed to process audio data")
				// 通过session的SendQueue发送错误消息
				if sess != nil {
					select {
					case sess.SendQueue <- map[string]interface{}{
						"type":    "error",
						"message": err.Error(),
					}:
					default:
						logger.Warnf("Session %s send queue is full, dropping error message", sessionID)
					}
				}
			}
		}
	}
}
