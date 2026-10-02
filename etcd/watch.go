package etcd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WatchEvent watch 收到的事件
type WatchEvent struct {
	Type  string // "PUT" 或 "DELETE"（最新状态）
	Value string // PUT 时的新值（已解码）；DELETE 时为空
}

var errWatchAuthExpired = errors.New("etcd watch 认证过期，已重置 token")

// watchResponse 是 etcd watch 流中每行 JSON 的结构
type watchResponse struct {
	Result struct {
		Created      bool              `json:"created"`
		Canceled     bool              `json:"canceled"`
		CancelReason string            `json:"cancel_reason"`
		Events       []json.RawMessage `json:"events"`
	} `json:"result"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Watch 监听 key 的最新状态，首次建立和重连后重新读取。
// 连续相同状态不重复发送；读取失败每秒重试，错误通过 errs 非阻塞发送。
// 仅发送 PUT/DELETE，PUT 的空值与 DELETE 的缺失状态不同；不重放历史事件。
func (c *Client) Watch(ctx context.Context, key string) (<-chan WatchEvent, <-chan error) {
	out := make(chan WatchEvent, 8)
	errs := make(chan error, 4)
	watchEvents, watchErrs := c.watchChanges(ctx, key)
	go func() {
		defer close(out)
		defer close(errs)
		var last WatchEvent
		known := false
		retry := time.NewTimer(time.Hour)
		retry.Stop()
		defer retry.Stop()
		var retryCh <-chan time.Time
		report := func(err error) {
			select {
			case errs <- err:
			default:
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-watchEvents:
				if !ok {
					return
				}
			case <-retryCh:
			case err, ok := <-watchErrs:
				if !ok {
					watchErrs = nil
				} else {
					report(err)
				}
				continue
			}
			readCtx, cancel := context.WithTimeout(ctx, c.timeout)
			value, exists, err := c.Lookup(readCtx, key)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				report(err)
				retry.Reset(time.Second)
				retryCh = retry.C
				continue
			}
			retry.Stop()
			retryCh = nil
			event := WatchEvent{Type: "PUT", Value: value}
			if !exists {
				event.Type = "DELETE"
			}
			if known && event == last {
				continue
			}
			select {
			case out <- event:
				last, known = event, true
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, errs
}

// watchChanges 维护底层连接，将创建监听和变更批次作为状态同步提示。
func (c *Client) watchChanges(ctx context.Context, key string) (<-chan struct{}, <-chan error) {
	events := make(chan struct{}, 1)
	errs := make(chan error, 4)

	go func() {
		defer close(events)
		defer close(errs)

		const (
			initBackoff = time.Second
			maxBackoff  = 30 * time.Second
		)
		backoff := initBackoff
		authImmediateRetryUsed := false

		for {
			// 检查 ctx 是否已取消
			select {
			case <-ctx.Done():
				return
			default:
			}

			err, connected := c.watchOnce(ctx, key, events)
			if err == nil {
				// ctx 取消导致的正常退出
				return
			}
			authExpired := errors.Is(err, errWatchAuthExpired)
			// 连接曾经成功过，说明 etcd 是健康的，重置退避时间
			if connected {
				backoff = initBackoff
			}
			if connected || !authExpired {
				authImmediateRetryUsed = false
			}

			// 发送错误，非阻塞
			select {
			case errs <- fmt.Errorf("etcd watch 断开，准备重连: %w", err):
			default:
			}

			// token 过期属于可恢复错误，清 token 后立即重连一次；若仍持续 401，再走退避。
			if authExpired && !authImmediateRetryUsed {
				authImmediateRetryUsed = true
				continue
			}

			// 退避等待后重连
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
		}
	}()

	return events, errs
}

// watchOnce 建立一次 watch 连接并持续读取事件，直到连接断开或 ctx 取消。
// 返回值：err=nil 表示 ctx 取消的正常退出；connected 表示本次连接是否曾成功建立或收到事件。
func (c *Client) watchOnce(ctx context.Context, key string, events chan<- struct{}) (err error, connected bool) {
	body, _ := json.Marshal(map[string]any{
		"create_request": map[string]any{
			"key":             b64(key),
			"progress_notify": false,
		},
	})

	if err := c.ensureToken(ctx); err != nil {
		return err, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint()+"/v3/watch", bytes.NewReader(body))
	if err != nil {
		return err, false
	}
	usedToken := c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false
		}
		return err, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && c.username != "" {
		// token 过期，清除后让外层立即重连一次并由 ensureToken 重新获取
		c.resetToken(usedToken)
		return errWatchAuthExpired, false
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("etcd watch 失败 %d: %s", resp.StatusCode, raw), false
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, connected
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg watchResponse
		if err := json.Unmarshal(line, &msg); err != nil {
			return err, connected
		}
		if msg.Error.Message != "" {
			return fmt.Errorf("etcd watch 服务端错误: %s", msg.Error.Message), connected
		}
		if msg.Result.Canceled {
			return fmt.Errorf("etcd watch 已取消: %s", msg.Result.CancelReason), connected
		}
		if msg.Result.Created || len(msg.Result.Events) > 0 {
			connected = true
			// 合并积压提示，消费者将读取最新状态。
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		if ctx.Err() != nil {
			return nil, connected
		}
		return err, connected
	}
	return fmt.Errorf("watch 连接已关闭"), connected
}
