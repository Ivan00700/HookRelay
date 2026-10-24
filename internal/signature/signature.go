package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	Header = "X-HookRelay-Signature"
	prefix = "sha256="
)

// Sign подписывает точные байты исходящего HTTP-тела.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return prefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify проверяет формат заголовка и подпись до обработки тела.
func Verify(body []byte, secret, value string) bool {
	if secret == "" || !strings.HasPrefix(value, prefix) {
		return false
	}

	// 1) Восстанавливаем байты подписи из заголовка.
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != sha256.Size*2 {
		return false
	}
	actual, err := hex.DecodeString(encoded)
	if err != nil {
		return false
	}

	// 2) Считаем HMAC над принятым телом и сравниваем MAC через hmac.Equal.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(actual, mac.Sum(nil))
}
