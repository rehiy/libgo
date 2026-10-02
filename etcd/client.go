// Package etcd 提供基于 etcd gRPC-gateway HTTP v3 API 的轻量客户端，
// 无 gRPC/protobuf 依赖，仅使用 Go 标准库。
package etcd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Client etcd HTTP v3 客户端
type Client struct {
	endpoints  []string
	username   string
	password   string
	timeout    time.Duration
	httpClient *http.Client
	robin      atomic.Uint64 // 轮询计数器，用于多节点负载均衡

	mu    sync.Mutex // 保护 token 及其初始化过程
	token string     // 缓存的 auth token
}

// Config 客户端配置
type Config struct {
	Endpoints   []string
	Username    string
	Password    string
	DialTimeout time.Duration
}

// New 创建 etcd 客户端
func New(cfg Config) *Client {
	timeout := cfg.DialTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		endpoints: cfg.Endpoints,
		username:  cfg.Username,
		password:  cfg.Password,
		timeout:   timeout,
		// 不设置全局 Timeout，避免 watch 长连接被定时切断；
		// 普通请求超时由 ctx 或 c.timeout 控制
		httpClient: &http.Client{},
	}
}

// Get 读取 key 的精确值，key 不存在时返回空字符串
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	value, _, err := c.Lookup(ctx, key)
	return value, err
}

// Lookup 读取 key 的精确值并返回是否存在，区分空字符串与缺失。
func (c *Client) Lookup(ctx context.Context, key string) (string, bool, error) {
	body, _ := json.Marshal(map[string]any{
		"key":   b64(key),
		"limit": 1, // 精确查询只需要一条结果
	})
	raw, err := c.do(ctx, "/v3/kv/range", body)
	if err != nil {
		return "", false, err
	}

	var result struct {
		Kvs []struct {
			Value string `json:"value"`
		} `json:"kvs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", false, fmt.Errorf("etcd Get 响应解析失败: %w", err)
	}
	if len(result.Kvs) == 0 {
		return "", false, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Kvs[0].Value)
	if err != nil {
		return "", false, fmt.Errorf("etcd Get value 解码失败: %w", err)
	}
	return string(decoded), true, nil
}

// Put 写入 key/value
func (c *Client) Put(ctx context.Context, key, value string) error {
	body, _ := json.Marshal(map[string]string{
		"key":   b64(key),
		"value": b64(value),
	})
	_, err := c.do(ctx, "/v3/kv/put", body)
	return err
}

// CompareAndPut 原子比较并写入。expected 为 nil 时只创建缺失 key；
// 非 nil 时比较值且要求 key 存在。返回 false 表示冲突，不执行写入。
func (c *Client) CompareAndPut(ctx context.Context, key string, expected, value []byte) (bool, error) {
	compare := []map[string]any{{"key": b64(key), "target": "VERSION", "result": "EQUAL", "version": "0"}}
	if expected != nil {
		compare = []map[string]any{
			{"key": b64(key), "target": "VERSION", "result": "GREATER", "version": "0"},
			{"key": b64(key), "target": "VALUE", "result": "EQUAL", "value": base64.StdEncoding.EncodeToString(expected)},
		}
	}
	body, _ := json.Marshal(map[string]any{
		"compare": compare,
		"success": []map[string]any{{"request_put": map[string]string{"key": b64(key), "value": base64.StdEncoding.EncodeToString(value)}}},
	})
	raw, err := c.do(ctx, "/v3/kv/txn", body)
	if err != nil {
		return false, err
	}
	var result struct {
		Succeeded bool `json:"succeeded"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false, fmt.Errorf("etcd txn 响应解析失败: %w", err)
	}
	return result.Succeeded, nil
}

// do 执行一次 HTTP POST 请求，若 ctx 无 deadline 则自动附加 c.timeout。
// 当配置了认证且收到 401 时，自动刷新 token 并重试一次。
func (c *Client) do(ctx context.Context, path string, body []byte) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	for attempt := range 2 {
		if err := c.ensureToken(ctx); err != nil {
			return nil, err
		}
		raw, statusCode, usedToken, err := c.doOnce(ctx, path, body)
		if err != nil {
			return nil, err
		}
		if statusCode == http.StatusUnauthorized && attempt == 0 && c.username != "" {
			c.resetToken(usedToken)
			continue
		}
		if statusCode >= 300 {
			return nil, fmt.Errorf("etcd %s 失败 %d: %s", path, statusCode, raw)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("etcd %s 认证失败", path) // unreachable
}

// doOnce 执行单次 HTTP POST 请求，返回响应体、状态码、本次使用的 token 和错误。
func (c *Client) doOnce(ctx context.Context, path string, body []byte) ([]byte, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint()+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", err
	}
	usedToken := c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, usedToken, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, usedToken, nil
}

// authenticate 调用 /v3/auth/authenticate 获取 token
func (c *Client) authenticate(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"name":     c.username,
		"password": c.password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint()+"/v3/auth/authenticate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("etcd authenticate 失败 %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("etcd authenticate 响应解析失败: %w", err)
	}
	return result.Token, nil
}

// ensureToken 确保开启认证配置时已缓存 token，无需认证或已有 token 时直接返回。
func (c *Client) ensureToken(ctx context.Context) error {
	if c.username == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return nil
	}
	token, err := c.authenticate(ctx)
	if err != nil {
		return err
	}
	c.token = token
	return nil
}

// resetToken 清除匹配的缓存 token，下次 ensureToken 会重新获取。
func (c *Client) resetToken(token string) {
	c.mu.Lock()
	if c.token == token {
		c.token = ""
	}
	c.mu.Unlock()
}

// endpoint 以轮询方式返回一个 endpoint，实现多节点负载均衡
func (c *Client) endpoint() string {
	if len(c.endpoints) == 0 {
		return "http://localhost:2379"
	}
	idx := c.robin.Add(1) % uint64(len(c.endpoints))
	return c.endpoints[idx]
}

// setAuth 设置请求的认证信息，并返回本次使用的 token。
func (c *Client) setAuth(req *http.Request) string {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	return token
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
