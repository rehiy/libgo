package secure

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP uses SHA1, six digits and a 30-second period.
const (
	totpPeriod = 30
	totpDigits = 6
)

// TOTPSecretGenerate 生成 Base32 编码的 TOTP 密钥
func TOTPSecretGenerate() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 TOTP 密钥失败: %w", err)
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(buf), "="), nil
}

// TOTPURI 生成可导入认证器 App 的 otpauth URI
func TOTPURI(issuer, username, secret string) string {
	label := url.PathEscape(issuer + ":" + username)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprintf("%d", totpDigits))
	query.Set("period", fmt.Sprintf("%d", totpPeriod))
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// TOTPValidate 验证 TOTP 验证码，允许前后一周期时间偏移
func TOTPValidate(secret string, code string) bool {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	code = strings.TrimSpace(code)
	if secret == "" || code == "" {
		return false
	}
	for _, offset := range []int64{-1, 0, 1} {
		if TOTPCode(secret, time.Now().Unix()/totpPeriod+offset) == code {
			return true
		}
	}
	return false
}

// TOTPCode 根据时间步生成 6 位验证码
func TOTPCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", binaryCode%1000000)
}
