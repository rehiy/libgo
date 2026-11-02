package websocket

import (
	"sync"
	"time"
)

// Pinger 表示支持发送 WebSocket ping 控制帧的连接。
type Pinger interface {
	Ping(data []byte) error
}

// KeepAlive 周期性发送 WebSocket ping 控制帧，直到 stop 被调用或 Ping 返回错误。
// 返回的 stop 可安全多次调用。
func KeepAlive(conn Pinger, interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := conn.Ping(nil); err != nil {
					return
				}
			}
		}
	}()

	return sync.OnceFunc(func() { close(done) })
}
