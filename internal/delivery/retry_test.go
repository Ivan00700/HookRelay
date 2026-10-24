package delivery

import (
	"errors"
	"testing"
)

func TestShouldRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		err    error
		want   bool
	}{
		{204, nil, false},
		{0, errors.New("connection refused"), true},
		{0, ErrInvalidRequest, false},
		{408, errors.New("request timeout"), true},
		{429, errors.New("too many requests"), true},
		{500, errors.New("server error"), true},
		{503, errors.New("unavailable"), true},
		{307, errors.New("redirect"), false},
		{400, errors.New("bad request"), false},
		{401, errors.New("unauthorized"), false},
		{404, errors.New("not found"), false},
		{422, errors.New("unprocessable"), false},
	} {
		if got := shouldRetry(tc.status, tc.err); got != tc.want {
			t.Fatalf("status=%d, err=%v: retry=%t, want %t", tc.status, tc.err, got, tc.want)
		}
	}
}
