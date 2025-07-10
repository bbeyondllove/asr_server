package router

import (
	"asr_server/internal/bootstrap"
	"asr_server/internal/handlers"
	"asr_server/internal/ws"

	"github.com/gin-gonic/gin"
)

// NewRouter 注册所有路由，返回 *gin.Engine
func NewRouter(deps *bootstrap.AppDependencies) *gin.Engine {
	ginRouter := gin.New()
	ginRouter.Use(gin.Recovery())
	// TODO: 根据需要注入 gin.Logger()

	// 注册基础路由
	ginRouter.GET("/ws", func(c *gin.Context) {
		ws.HandleWebSocket(c.Writer, c.Request, deps.SessionManager, deps.GlobalRecognizer)
	})
	ginRouter.GET("/health", handlers.HealthHandler(deps))
	ginRouter.GET("/stats", handlers.StatsHandler(deps))

	// 静态文件服务
	ginRouter.Static("/static", "./static")
	ginRouter.StaticFile("/", "./static/index.html")

	// 注册声纹识别路由（如果启用）
	if deps.SpeakerHandler != nil {
		deps.SpeakerHandler.RegisterRoutes(ginRouter)
	}

	return ginRouter
}
