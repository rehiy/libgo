package websocket

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rehiy/libgo/logman"
)

// ClientConn WebSocket 客户端连接
type ClientConn struct {
	*Conn
}

// DialOptions WebSocket 拨号选项，零值使用 gorilla 默认拨号器的设置。
type DialOptions struct {
	Header           http.Header   // 握手请求头，可携带 Origin、Authorization 等
	Subprotocols     []string      // 子协议列表
	HandshakeTimeout time.Duration // 握手超时，0 表示使用默认值
	ReadBufferSize   int           // 读缓冲大小，0 表示使用默认值
	WriteBufferSize  int           // 写缓冲大小，0 表示使用默认值
}

// DialContext 创建 WebSocket 客户端连接，支持 context 取消。
// 握手被服务端拒绝时仍会返回握手响应（可据此区分 401/403 与网络故障）；响应体已由 gorilla 处理，无需关闭。
func DialContext(ctx context.Context, url string, opt DialOptions) (*ClientConn, *http.Response, error) {
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = opt.Subprotocols
	if opt.HandshakeTimeout > 0 {
		dialer.HandshakeTimeout = opt.HandshakeTimeout
	}
	if opt.ReadBufferSize > 0 {
		dialer.ReadBufferSize = opt.ReadBufferSize
	}
	if opt.WriteBufferSize > 0 {
		dialer.WriteBufferSize = opt.WriteBufferSize
	}

	ws, resp, err := dialer.DialContext(ctx, url, opt.Header)
	if err != nil {
		return nil, resp, err
	}
	return &ClientConn{newConn(ws)}, resp, nil
}

// NewClient 创建 WebSocket 客户端连接
func NewClient(url, protocol, origin string) (*ClientConn, error) {
	logman.Info("connect to server", "url", url)

	opt := DialOptions{Header: http.Header{}}
	if origin != "" {
		opt.Header.Set("Origin", origin)
	}
	if protocol != "" {
		opt.Subprotocols = []string{protocol}
	}

	conn, _, err := DialContext(context.Background(), url, opt)
	if err != nil {
		logman.Error("connect failed", "error", err)
		return nil, err
	}
	return conn, nil
}
