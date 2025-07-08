package handlers

import (
	"asr_server/internal/bootstrap"
	"time"

	"github.com/gin-gonic/gin"
)

// StatsHandler 统计信息接口（依赖注入）
func StatsHandler(deps *bootstrap.AppDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats := map[string]interface{}{
			"timestamp": time.Now().Format(time.RFC3339),
		}
		if deps.VADPool != nil {
			stats["vad_pool"] = deps.VADPool.GetStats()
		}
		if deps.SessionManager != nil {
			stats["sessions"] = deps.SessionManager.GetStats()
		}
		if deps.RateLimiter != nil {
			stats["rate_limit"] = deps.RateLimiter.GetStats()
		}
		c.JSON(200, stats)
	}
}
