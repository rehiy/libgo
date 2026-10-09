package websocket

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	TextMessage   = websocket.TextMessage   // = 1
	BinaryMessage = websocket.BinaryMessage // = 2
)

// Conn gorilla/websocket 的流式包装，提供并发安全的 Read/Write 和原生 Ping
type Conn struct {
	ws           *websocket.Conn
	wmu          sync.Mutex    // guards WriteMessage / WriteJSON
	rmu          sync.Mutex    // guards NextReader / ReadJSON
	rdBuf        io.Reader     // current message reader (stream shim)
	msgType      int           // TextMessage or BinaryMessage
	binaryOnly   atomic.Bool   // Read 仅接收二进制消息，忽略文本消息
	writeTimeout time.Duration // write deadline timeout
}

func newConn(ws *websocket.Conn) *Conn {
	return &Conn{ws: ws, msgType: TextMessage}
}

func newConnWithWriteTimeout(ws *websocket.Conn, timeout time.Duration) *Conn {
	conn := newConn(ws)
	conn.writeTimeout = timeout
	return conn
}

// SetMessageType 设置写消息类型（如二进制转发场景切换 BinaryMessage）
func (c *Conn) SetMessageType(t int) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.msgType = t
}

// SetReadBinaryOnly 设置 Read 是否只接收二进制消息；开启后文本消息会被整条丢弃。
// 适用于承载二进制协议的连接，避免对端误发的文本帧污染字节流。
func (c *Conn) SetReadBinaryOnly(on bool) {
	c.binaryOnly.Store(on)
}

// Read 流式读，跨多消息拼接
func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for {
		if c.rdBuf != nil {
			n, err := c.rdBuf.Read(p)
			if n > 0 {
				if err == io.EOF {
					c.rdBuf = nil
				}
				return n, nil
			}
			if err == io.EOF {
				c.rdBuf = nil
				continue
			}
			return 0, err
		}
		messageType, r, err := c.ws.NextReader()
		if err != nil {
			return 0, err
		}
		if c.binaryOnly.Load() && messageType != websocket.BinaryMessage {
			continue
		}
		c.rdBuf = r
	}
}

// Write 每次调用发一条 WS 消息，wmu 保证与并发 Write 互斥
func (c *Conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.writeTimeout > 0 {
		_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	if err := c.ws.WriteMessage(c.msgType, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ReadJSON 读取 JSON 消息
func (c *Conn) ReadJSON(v any) error {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	return c.ws.ReadJSON(v)
}

// WriteJSON 写入 JSON 消息
func (c *Conn) WriteJSON(v any) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.writeTimeout > 0 {
		_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	return c.ws.WriteJSON(v)
}

// Ping 发送 WS ping 控制帧（gorilla WriteControl 自身并发安全，无需 wmu）
func (c *Conn) Ping(data []byte) error {
	timeout := c.writeTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return c.ws.WriteControl(websocket.PingMessage, data, time.Now().Add(timeout))
}

// Close 尽力发送关闭帧后关闭连接。
// WriteControl 可与其他方法并发调用，不占用 wmu，避免写阻塞时 Close 无法解除阻塞。
func (c *Conn) Close() error {
	_ = c.ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return c.ws.Close()
}

// Die 发送消息并关闭连接
func (c *Conn) Die(msg string) {
	_, _ = c.Write([]byte(msg))
	_ = c.Close()
}
