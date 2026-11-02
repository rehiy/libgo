// Package certify 提供证书自动签发和管理
//
// 基于 golang.org/x/crypto/acme/autocert 扩展，添加 DNS-01 验证支持
package certify

import (
	"context"
	"crypto/tls"
	"net/http"
	"slices"
	"sync"

	"github.com/libdns/libdns"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// ACME 目录 URL
const (
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	Buypass               = "https://api.buypass.com/acme/directory"
	ZeroSSL               = "https://acme.zerossl.com/v2/DV90"
	Google                = "https://dv.acme-v02.api.pki.goog/directory"
)

// Manager 证书管理器
type Manager struct {
	*autocert.Manager

	// DNS-01 验证需要
	DNSProvider interface {
		libdns.RecordAppender
		libdns.RecordDeleter
	}

	// 日志记录器
	Logger interface {
		Info(msg string, args ...any)
		Error(msg string, args ...any)
	}

	// 事件回调
	OnEvent func(event string, data map[string]any)

	client *acme.Client
	mu     sync.Mutex
}

// New 创建证书管理器
func New(email string, cache autocert.Cache) *Manager {
	return &Manager{
		Manager: &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			Email:  email,
			Cache:  cache,
		},
	}
}

// NewWithDirectory 创建证书管理器（指定 ACME 目录）
func NewWithDirectory(email string, cache autocert.Cache, directoryURL string) *Manager {
	m := New(email, cache)
	m.Manager.Client = &acme.Client{DirectoryURL: directoryURL}
	return m
}

// GetCertificate 获取证书（支持通配符域名）
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if isWildcard(hello.ServerName) {
		return m.getCertDNS01(hello.ServerName)
	}
	return m.Manager.GetCertificate(hello)
}

// GetCertificateForDomains 获取指定域名的证书（支持多域名 SAN 和通配符）
func (m *Manager) GetCertificateForDomains(domains []string) (*tls.Certificate, error) {
	if len(domains) == 0 {
		return nil, ErrNoDomains
	}

	if slices.ContainsFunc(domains, isWildcard) {
		return m.getCertDNS01(domains...)
	}

	if len(domains) == 1 {
		return m.getSingleCert(domains[0])
	}
	return m.getCertDNS01(domains...)
}

// RevokeCert 吊销证书
func (m *Manager) RevokeCert(ctx context.Context, domain string) error {
	client, err := m.getClient(ctx)
	if err != nil {
		return err
	}
	cert, err := m.loadCert(domain)
	if err != nil {
		return err
	}
	// 使用账户密钥吊销证书
	return client.RevokeCert(ctx, client.Key, cert.Certificate[0], acme.CRLReasonUnspecified)
}

// TLSConfig 返回 TLS 配置
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: m.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
	}
}

// HTTPHandler 返回 HTTP-01 验证处理器
func (m *Manager) HTTPHandler(fallback http.Handler) http.Handler {
	return m.Manager.HTTPHandler(fallback)
}

// emitEvent 触发事件
func (m *Manager) emitEvent(event string, data map[string]any) {
	if m.OnEvent != nil {
		m.OnEvent(event, data)
	}
}

// isWildcard 检查是否为通配符域名
func isWildcard(domain string) bool {
	return len(domain) > 2 && domain[0] == '*' && domain[1] == '.'
}
