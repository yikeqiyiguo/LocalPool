package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// resp 统一响应体
type resp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, resp{Code: 0, Msg: "ok", Data: data})
}

func fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, resp{Code: code, Msg: msg})
}

func failMsg(c *gin.Context, msg string) {
	fail(c, http.StatusBadRequest, 1, msg)
}

func queryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// corsMiddleware 允许 Vite DevServer 跨域
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-Agent-Key, X-API-Key")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// sse 设置 SSE 响应头并立即 Flush
func sseHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()
}

// writeSSE 写入一个 SSE 事件
func writeSSE(c *gin.Context, event string, payload []byte) {
	if event != "" {
		_, _ = c.Writer.WriteString("event: " + event + "\n")
	}
	_, _ = c.Writer.WriteString("data: " + string(payload) + "\n\n")
	c.Writer.Flush()
}
