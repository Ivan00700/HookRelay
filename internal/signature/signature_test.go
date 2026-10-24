package signature

import (
	"bytes"
	"strings"
	"testing"
)

func TestSignRFC4231(t *testing.T) {
	// контрольный пример из RFC 4231 - проверяем HMAC-SHA256 независимо
	secret := string(bytes.Repeat([]byte{0x0b}, 20))
	want := "sha256=b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
	if got := Sign([]byte("Hi There"), secret); got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	if !Verify([]byte("Hi There"), secret, want) {
		t.Fatal("valid RFC signature rejected")
	}
}

func TestVerify(t *testing.T) {
	body := []byte(`{"order_id":42}`)
	secret := "receiver-secret"
	valid := Sign(body, secret)
	tests := []struct {
		name   string
		body   []byte
		secret string
		value  string
		want   bool
	}{
		{"valid", body, secret, valid, true},
		{"changed payload", []byte(`{"order_id":43}`), secret, valid, false},
		{"different whitespace", []byte(`{ "order_id":42}`), secret, valid, false},
		{"wrong secret", body, "other-secret", valid, false},
		{"missing", body, secret, "", false},
		{"wrong algorithm", body, secret, strings.Replace(valid, "sha256=", "sha1=", 1), false},
		{"missing prefix", body, secret, strings.TrimPrefix(valid, "sha256="), false},
		{"short value", body, secret, "sha256=ab", false},
		{"invalid hex", body, secret, "sha256=" + strings.Repeat("z", 64), false},
		{"wrong digest", body, secret, "sha256=" + strings.Repeat("0", 64), false},
		{"extra data", body, secret, valid + "00", false},
		{"empty secret", body, "", Sign(body, ""), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Verify(test.body, test.secret, test.value); got != test.want {
				t.Fatalf("Verify = %v, want %v", got, test.want)
			}
		})
	}
}
