package server

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// APIServer Web/Agent HTTP 服务
type APIServer struct {
	eng    *Engine
	hub    *LogHub
	secret string // 访问密钥：非空时 /api 与 /agent 端点须携带匹配的密钥
}

// NewAPIServer 创建 HTTP 服务；secret 为空表示不鉴权（仅限可信网络）
func NewAPIServer(e *Engine, secret string) *APIServer {
	return &APIServer{eng: e, hub: e.hub, secret: secret}
}

// BuildRouter 构建全部 HTTP 路由（API + Agent + 前端静态资源）
func (a *APIServer) BuildRouter(staticFS fs.FS) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware())

	api := r.Group("/api")
	api.Use(a.requireKey())

	// 大盘/节点
	api.GET("/overview", a.handleOverview)
	api.GET("/nodes", a.handleNodes)
	api.GET("/nodes/:id", a.handleNode)

	// 任务
	api.GET("/tasks", a.handleListTasks)
	api.POST("/tasks", a.handleCreateTask)
	api.GET("/tasks/summary", a.handleTaskSummary)
	api.GET("/tasks/:id", a.handleGetTask)
	api.DELETE("/tasks/:id", a.handleDeleteTask)
	api.POST("/tasks/:id/cancel", a.handleCancelTask)
	api.POST("/tasks/:id/retry", a.handleRetryTask)
	api.GET("/tasks/:id/logs", a.handleTaskLogs)
	api.GET("/tasks/:id/logs/stream", a.handleTaskLogStream)

	// 统计
	api.GET("/stats/timeline", a.handleTimeline)

	// 设置
	api.GET("/config", a.handleGetConfig)
	api.PUT("/config", a.handlePutConfig)

	// Agent 专用端点（除 /agent/ping 探活外均需鉴权）
	ag := r.Group("/agent")
	ag.Use(a.requireKey())
	ag.POST("/register", a.handleAgentRegister)
	ag.POST("/heartbeat", a.handleAgentHeartbeat)
	ag.POST("/logs", a.handleAgentLogs)
	ag.POST("/result", a.handleAgentResult)

	// 探活端点保持匿名，供 Agent/监控探测服务器是否可达
	r.GET("/agent/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	a.registerStatic(r, staticFS)
	return r
}

// requireKey 鉴权中间件：配置了访问密钥时，请求须携带匹配的
// X-Agent-Key(Agent) 或 X-API-Key(Web 控制台)。密钥为空则跳过校验。
func (a *APIServer) requireKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.secret == "" {
			c.Next()
			return
		}
		key := c.GetHeader("X-Agent-Key")
		if key == "" {
			key = c.GetHeader("X-API-Key")
		}
		if len(key) == 0 || subtle.ConstantTimeCompare([]byte(key), []byte(a.secret)) != 1 {
			fail(c, http.StatusUnauthorized, 401, "缺少或无效的访问密钥")
			c.Abort()
			return
		}
		c.Next()
	}
}

// registerStatic 提供内嵌前端静态资源，未命中文件时回退 index.html（SPA）
func (a *APIServer) registerStatic(r *gin.Engine, staticFS fs.FS) {
	if staticFS == nil {
		return
	}
	sub, err := fs.Sub(staticFS, "dist")
	if err != nil || sub == nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/agent/") {
			fail(c, http.StatusNotFound, 404, "not found")
			return
		}
		rel := strings.TrimPrefix(p, "/")
		if rel == "" {
			rel = "index.html"
		}
		if _, err := fs.Stat(sub, rel); err == nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		data, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "static not built")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
}

var _ = time.Now
