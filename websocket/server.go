package websocket

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ServerConfig WebSocket 服务端配置
type ServerConfig struct {
	AllowedOrigins   []string      // 允许的 Origin 列表，支持通配符 *
	AllowEmptyOrigin bool          // 配置 AllowedOrigins 后是否允许空 Origin
	ReadLimit        int64         // 单条消息最大读取字节数，0 表示不限制
	ReadTimeout      time.Duration // 读取超时，0 表示不设置
	WriteTimeout     time.Duration // 写控制帧超时，0 表示使用默认值
	PongTimeout      time.Duration // pong 等待超时，0 表示不设置
}

// ServerConn WebSocket 服务端连接
type ServerConn struct {
	*Conn
}

// Handler 创建 WebSocket 处理器（gin HandlerFunc）
// 使用方式：router.GET("/ws", config.Handler(func(conn *ServerConn) { ... }))
func (c *ServerConfig) Handler(handler func(*ServerConn)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !c.CheckOrigin(ctx.GetHeader("Origin")) {
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		ws, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
		if err != nil {
			return
		}
		c.applyConnOptions(ws)
		conn := &ServerConn{newConnWithWriteTimeout(ws, c.WriteTimeout)}
		handler(conn)
	}
}

// CorsMiddleware CORS 中间件
func (c *ServerConfig) CorsMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")
		if c.CheckOrigin(origin) {
			if origin != "" {
				ctx.Header("Access-Control-Allow-Origin", origin)
			}
			ctx.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			ctx.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			ctx.Header("Access-Control-Allow-Credentials", "true")
		}
		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}
		ctx.Next()
	}
}

// CheckOrigin 检查 Origin 是否允许
func (c *ServerConfig) CheckOrigin(origin string) bool {
	if len(c.AllowedOrigins) == 0 {
		return true
	}
	if origin == "" {
		return c.AllowEmptyOrigin
	}
	for _, allowed := range c.AllowedOrigins {
		if c.matchOrigin(origin, allowed) {
			return true
		}
	}
	return false
}

func (c *ServerConfig) applyConnOptions(ws *websocket.Conn) {
	if c.ReadLimit > 0 {
		ws.SetReadLimit(c.ReadLimit)
	}
	if c.ReadTimeout > 0 {
		_ = ws.SetReadDeadline(time.Now().Add(c.ReadTimeout))
	}
	if c.PongTimeout > 0 {
		_ = ws.SetReadDeadline(time.Now().Add(c.PongTimeout))
		ws.SetPongHandler(func(string) error {
			return ws.SetReadDeadline(time.Now().Add(c.PongTimeout))
		})
	}
}

// matchOrigin 检查 origin 是否匹配允许的模式（支持通配符 *）
func (c *ServerConfig) matchOrigin(origin, pattern string) bool {
	if origin == pattern {
		return true
	}
	if prefix, suffix, ok := strings.Cut(pattern, "*"); ok {
		return strings.HasPrefix(origin, prefix) && strings.HasSuffix(origin, suffix)
	}
	return false
}
