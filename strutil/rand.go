package strutil

import (
	crand "crypto/rand"

	"github.com/google/uuid"
)

// Rand 生成密码学安全的随机字符串（0-9A-Za-z），可用于令牌等凭据场景。
// 熵源不可用时直接 panic，不会退化为可预测的输出。
func Rand(length uint) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const maxByte = 248 // 62 的最大整数倍，拒绝采样避免取模偏差
	buf := make([]byte, 0, length)
	raw := make([]byte, length)
	for uint(len(buf)) < length {
		if _, err := crand.Read(raw); err != nil {
			panic("strutil.Rand: 系统熵源不可用: " + err.Error())
		}
		for _, b := range raw {
			if b >= maxByte {
				continue
			}
			buf = append(buf, alphabet[b%62])
			if uint(len(buf)) == length {
				break
			}
		}
	}
	return string(buf)
}

// NewString 生成基于 UUID v7 的唯一字符串
func NewString() string {
	tid, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return tid.String()
}
