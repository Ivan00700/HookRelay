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

type Sender struct {
	client *http.Client
}

func NewSender(client *http.Client) *Sender {
	if client == nil {
		client = http.DefaultClient
	}

	// A redirect is a non-2xx response, not permission to send the payload elsewhere.
	configuredClient := *client
	configuredClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Sender{client: &configuredClient}
}

func (s *Sender) Send(ctx context.Context, event domain.Event, endpoint domain.Endpoint) (int, error) {
	if strings.TrimSpace(endpoint.Secret) == "" {
		return 0, fmt.Errorf("%w: endpoint secret is required", ErrInvalidRequest)
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		endpoint.URL,
		bytes.NewReader(event.Payload),
	)
	if err != nil {
		return 0, fmt.Errorf("create delivery request: %w: %v", ErrInvalidRequest, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-HookRelay-Event-ID", event.ID)
	request.Header.Set(signature.Header, signature.Sign(event.Payload, endpoint.Secret))

	response, err := s.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("send delivery request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response.StatusCode, fmt.Errorf("delivery returned HTTP %d", response.StatusCode)
	}
	return response.StatusCode, nil
}
