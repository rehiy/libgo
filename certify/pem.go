package certify

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

// keyToPEM 编码私钥为 PEM
func keyToPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
}

// keyFromPEM 从 PEM 解析私钥
func keyFromPEM(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM")
	}

	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key.(crypto.Signer), nil
	}

	return nil, errors.New("invalid private key")
}

// certToPEM 编码证书为 PEM
func certToPEM(cert *tls.Certificate) []byte {
	var buf bytes.Buffer

	// 编码私钥
	if key, ok := cert.PrivateKey.(*ecdsa.PrivateKey); ok {
		b, _ := x509.MarshalECPrivateKey(key)
		pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	}

	// 编码证书链
	for _, b := range cert.Certificate {
		pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: b})
	}

	return buf.Bytes()
}

// tlsCertFromPEM 从 PEM 解析证书
func tlsCertFromPEM(data []byte) (*tls.Certificate, error) {
	var keyPEM, certPEM []byte
	var block *pem.Block

	for {
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		switch {
		case block.Type == "EC PRIVATE KEY" || block.Type == "RSA PRIVATE KEY" || block.Type == "PRIVATE KEY":
			keyPEM = pem.EncodeToMemory(block)
		case block.Type == "CERTIFICATE":
			certPEM = append(certPEM, pem.EncodeToMemory(block)...)
		}
	}

	if len(keyPEM) == 0 || len(certPEM) == 0 {
		return nil, errors.New("missing key or certificate")
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	if cert.Leaf == nil {
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, err
		}
	}

	return &cert, nil
}

// PEMParse 解析第一个 CERTIFICATE block，跳过私钥等其他 block。
// 内容缺失或证书无效时返回 nil。
func PEMParse(data []byte) *x509.Certificate {
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil
		}
		if block.Type == "CERTIFICATE" {
			cert, _ := x509.ParseCertificate(block.Bytes)
			return cert
		}
	}
	return nil
}
