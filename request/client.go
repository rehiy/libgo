package request

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rehiy/libgo/logman"
)

type Client struct {
	Method  string            `note:"请求方法 GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|CONNECT|TRACE"`
	Url     string            `note:"请求地址"`
	Data    string            `note:"请求数据"`
	Headers map[string]string `note:"请求头"`
	Timeout time.Duration     `note:"超时时间"`
}

func (c *Client) Request() ([]byte, error) {
	return c.RequestContext(context.Background())
}

// RequestContext performs the request with cancellation, preserving Client configuration.
func (c *Client) RequestContext(ctx context.Context) ([]byte, error) {
	resp, err := c.DoContext(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// DoContext returns the response; the caller must close its body.
// A nil client uses Client.Timeout; a supplied client keeps its own configuration.
func (c *Client) DoContext(ctx context.Context, client *http.Client) (*http.Response, error) {
	var body io.Reader
	if c.Data != "" {
		body = strings.NewReader(c.Data)
	}

	req, err := http.NewRequestWithContext(ctx, c.Method, c.Url, body)
	if err != nil {
		return nil, err
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}

	logman.Debug("http request", "method", c.Method, "url", c.Url, "data", c.Data)

	if client == nil {
		client = &http.Client{Timeout: c.Timeout}
	}
	return client.Do(req)
}

func (c *Client) JsonRequest() ([]byte, error) {
	if c.Headers == nil {
		c.Headers = make(map[string]string)
	}
	if c.isBodyMethod() && c.Headers["Content-Type"] == "" {
		c.Headers["Content-Type"] = "application/json"
	}

	return c.Request()
}

func (c *Client) TextRequest() (string, error) {
	if c.Headers == nil {
		c.Headers = make(map[string]string)
	}
	if c.isBodyMethod() && c.Headers["Content-Type"] == "" {
		c.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	}

	body, err := c.Request()
	return string(body), err
}

func (c *Client) isBodyMethod() bool {
	return strings.Contains("POST,PUT,PATCH", c.Method)
}
