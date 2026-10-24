package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const Header = "X-HookRelay-Signature"

const prefix = "sha256="

// Sign signs the exact bytes that will be sent in the HTTP request body.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return prefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks the signature before the receiver processes the body.
func Verify(body []byte, secret, value string) bool {
	if secret == "" || !strings.HasPrefix(value, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != sha256.Size*2 {
		return false
	}
	actual, err := hex.DecodeString(encoded)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(actual, mac.Sum(nil))
}
