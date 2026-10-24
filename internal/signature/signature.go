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

// подписывает точные байты исходящего HTTP-тела
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return prefix + hex.EncodeToString(mac.Sum(nil))
}

// проверяет формат заголовка и подпись до обработки тела
func Verify(body []byte, secret, value string) bool {
	if secret == "" || !strings.HasPrefix(value, prefix) {
		return false
	}

	// восстанавливаем байты подписи из заголовка
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != sha256.Size*2 {
		return false
	}
	actual, err := hex.DecodeString(encoded)
	if err != nil {
		return false
	}

	// считаем HMAC над принятым телом - сравниваем MAC без утечки через время
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(actual, mac.Sum(nil))
}
