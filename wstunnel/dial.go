package wstunnel

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"github.com/rehiy/libgo/websocket"
)

// ErrUnauthorized 表示服务方拒绝了凭据（令牌无效或已被吊销），重试没有意义。
var ErrUnauthorized = errors.New("wstunnel: 服务方拒绝了凭据")

const (
	handshakeTimeout = 15 * time.Second
	minBackoff       = time.Second
	maxBackoff       = 60 * time.Second
	stableAfter      = 30 * time.Second // 连接稳定超过该时长后，下次重连从最小退避重新开始
)

// DialOptions 描述一次拨号。
type DialOptions struct {
	URL    string      // 服务方地址，支持 ws / wss / http / https
	Header http.Header // 握手请求头（携带 Authorization，令牌不进入 URL）
}

// Dial 主动连接服务方并建立隧道会话（拨号方，作为 yamux Server 接受流）。
func Dial(ctx context.Context, opt DialOptions) (*Session, error) {
	target, err := webSocketURL(opt.URL)
	if err != nil {
		return nil, err
	}
	conn, resp, err := websocket.DialContext(ctx, target, websocket.DialOptions{
		Header:           opt.Header,
		HandshakeTimeout: handshakeTimeout,
		ReadBufferSize:   32 << 10,
		WriteBufferSize:  32 << 10,
	})
	if err != nil {
		if resp != nil {
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return nil, fmt.Errorf("%w（HTTP %d）", ErrUnauthorized, resp.StatusCode)
			}
			return nil, fmt.Errorf("握手失败（HTTP %d）: %w", resp.StatusCode, err)
		}
		return nil, err
	}
	conn.SetMessageType(websocket.BinaryMessage)
	conn.SetReadBinaryOnly(true) // 与历史行为一致：忽略对端误发的文本帧
	return newSession(conn, false)
}

// MaintainOptions 描述常连循环。
type MaintainOptions struct {
	Dial         func(ctx context.Context) (*Session, error) // 每次重连调用，应使用最新凭据
	Serve        func(s *Session)                            // 在会话上提供服务，须在会话关闭后返回
	OnConnect    func(s *Session)                            // 可选：连接建立
	OnDisconnect func(s *Session)                            // 可选：连接断开
	OnError      func(err error)                             // 可选：拨号失败
}

// Maintain 保持与服务方的常连：断线后指数退避加抖动重连。
// ctx 取消返回 ctx.Err()；服务方拒绝凭据时返回 ErrUnauthorized，由调用方决定是否重新注册。
func Maintain(ctx context.Context, opt MaintainOptions) error {
	backoff := minBackoff

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		sess, err := opt.Dial(ctx)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			if opt.OnError != nil {
				opt.OnError(err)
			}
			if !Sleep(ctx, jitter(backoff)) {
				return ctx.Err()
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		startedAt := time.Now()
		if opt.OnConnect != nil {
			opt.OnConnect(sess)
		}

		served := make(chan struct{})
		go func() {
			defer close(served)
			opt.Serve(sess)
		}()
		select {
		case <-sess.Done():
		case <-ctx.Done():
		case <-served:
		}
		_ = sess.Close()
		<-served

		if opt.OnDisconnect != nil {
			opt.OnDisconnect(sess)
		}
		if time.Since(startedAt) > stableAfter {
			backoff = minBackoff // 稳定连接过一段时间后，下次从头退避
		}
		if !Sleep(ctx, jitter(backoff)) {
			return ctx.Err()
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// ─── 辅助函数 ───

// webSocketURL 将服务方地址规范为 ws/wss 地址
func webSocketURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("无效的服务方地址: %q", raw)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("不支持的服务方地址协议: %q", u.Scheme)
	}
	return u.String(), nil
}

// jitter 在 [d/2, d] 内取随机值，避免服务方重载后所有拨号方同时重连
func jitter(d time.Duration) time.Duration {
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half)
}

// Sleep 可被 ctx 取消的等待；返回 false 表示 ctx 已取消
func Sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
