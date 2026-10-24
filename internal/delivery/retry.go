package delivery

import "errors"

func shouldRetry(status int, err error) bool {
	if err == nil || errors.Is(err, ErrInvalidRequest) {
		return false
	}
	return status == 0 || status == 408 || status == 429 || (status >= 500 && status <= 599)
}
