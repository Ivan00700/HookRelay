package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/signature"
)

const requestTimeout = 3 * time.Second

var ErrInvalidRequest = errors.New("invalid delivery request")

// Sender выполняет одну HTTP-отправку с подписью и таймаутом.
type Sender struct {
	client *http.Client
}

func NewSender(client *http.Client) *Sender {
	if client == nil {
		client = http.DefaultClient
	}

	// Редирект считаем ответом получателя и не пересылаем тело на другой адрес.
	httpClient := *client
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Sender{client: &httpClient}
}

func (s *Sender) Send(ctx context.Context, event domain.Event, endpoint domain.Endpoint) (int, error) {
	if strings.TrimSpace(endpoint.Secret) == "" {
		return 0, fmt.Errorf("%w: endpoint secret is required", ErrInvalidRequest)
	}
	// 1) Готовим POST с тем же payload, который будет подписан.
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		endpoint.URL,
		bytes.NewReader(event.Payload),
	)
	if err != nil {
		return 0, fmt.Errorf("create delivery request: %w: %v", ErrInvalidRequest, err)
	}
	// 2) Добавляем служебные заголовки, не передавая сам секрет.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HookRelay-Event-ID", event.ID)
	req.Header.Set(signature.Header, signature.Sign(event.Payload, endpoint.Secret))

	// 3) Выполняем запрос и возвращаем HTTP-код вместе с ошибкой.
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send delivery request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("delivery returned HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
