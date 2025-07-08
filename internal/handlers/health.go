package handlers

import (
	"asr_server/internal/bootstrap"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthHandler 健康检查接口（依赖注入）
func HealthHandler(deps *bootstrap.AppDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		components := make(map[string]interface{})

		if deps.VADPool != nil {
			components["vad_pool"] = deps.VADPool.GetStats()
		} else {
			components["vad_pool"] = map[string]interface{}{"status": "not_initialized"}
		}
		if deps.SessionManager != nil {
			components["sessions"] = deps.SessionManager.GetStats()
		} else {
			components["sessions"] = map[string]interface{}{"status": "not_initialized"}
		}
		if deps.RateLimiter != nil {
			components["rate_limit"] = deps.RateLimiter.GetStats()
		} else {
			components["rate_limit"] = map[string]interface{}{"status": "not_initialized"}
		}
		if deps.SpeakerManager != nil {
			components["speaker"] = deps.SpeakerManager.GetStats()
		} else {
			components["speaker"] = map[string]interface{}{"status": "disabled"}
		}

		status := "healthy"
		if deps.VADPool == nil || deps.SessionManager == nil || deps.RateLimiter == nil {
			status = "initializing"
			c.Status(503)
		}

		health := map[string]interface{}{
			"status":     status,
			"timestamp":  time.Now().Format(time.RFC3339),
			"components": components,
		}
		c.JSON(200, health)
	}
}
