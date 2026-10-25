package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"hookrelay/internal/domain"
)

type options struct {
	baseURL     string
	endpointURL string
	secret      string
	count       int
	workers     int
	timeout     time.Duration
}

type summary struct {
	Accepted   int     `json:"accepted"`
	Rejected   int     `json:"rejected"`
	Delivered  int     `json:"delivered"`
	Failed     int     `json:"failed"`
	Unfinished int     `json:"unfinished"`
	Seconds    float64 `json:"seconds"`
}

func run(ctx context.Context, client *http.Client, config options) (report summary, runErr error) {
	started := time.Now()
	defer func() { report.Seconds = time.Since(started).Seconds() }()
	if config.count < 1 || config.count > 10000 || config.workers < 1 || config.workers > 64 || config.timeout <= 0 {
		return report, errors.New("проверьте count, concurrency и timeout")
	}
	for _, address := range []string{config.baseURL, config.endpointURL} {
		parsed, err := url.Parse(address)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return report, errors.New("base-url и endpoint-url должны быть абсолютными HTTP(S) адресами")
		}
	}
	if strings.TrimSpace(config.secret) == "" {
		return report, errors.New("задайте secret или WEBHOOK_SECRET")
	}
	base := strings.TrimRight(config.baseURL, "/")
	var endpoint domain.Endpoint
	if err := exchange(ctx, client, http.MethodPost, base+"/api/v1/endpoints", domain.CreateEndpointInput{
		Name: "load-check", URL: config.endpointURL, Secret: config.secret,
	}, &endpoint); err != nil {
		return report, fmt.Errorf("создание endpoint: %w", err)
	}

	// число событий ограничено - каждый воркер возвращает ID или пустую строку при ошибке
	jobs := make(chan int, config.count)
	results := make(chan string, config.count)
	for index := 0; index < config.count; index++ {
		jobs <- index
	}
	close(jobs)
	var wg sync.WaitGroup
	wg.Add(config.workers)
	for worker := 0; worker < config.workers; worker++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				payload := json.RawMessage(fmt.Sprintf(`{"index":%d}`, index))
				input := domain.CreateEventInput{EndpointID: endpoint.ID, Type: "load.test", Payload: payload}
				var event domain.Event
				if err := exchange(ctx, client, http.MethodPost, base+"/api/v1/events", input, &event); err != nil {
					results <- ""
					continue
				}
				results <- event.ID
			}
		}()
	}
	wg.Wait()
	close(results)
	ids := make(map[string]bool)
	for id := range results {
		if id == "" {
			report.Rejected++
			continue
		}
		report.Accepted++
		ids[id] = true
	}
	report.Unfinished = report.Accepted

	// ответ о приёме не означает доставку - ждём конечных статусов только своих событий
	for report.Unfinished > 0 {
		var events []domain.Event
		if err := exchange(ctx, client, http.MethodGet, base+"/api/v1/events", nil, &events); err != nil {
			return report, fmt.Errorf("чтение статусов: %w", err)
		}
		report.Delivered, report.Failed = 0, 0
		for _, event := range events {
			if !ids[event.ID] {
				continue
			}
			if event.Status == domain.DeliveryStatusDelivered {
				report.Delivered++
			}
			if event.Status == domain.DeliveryStatusFailed {
				report.Failed++
			}
		}
		report.Unfinished = report.Accepted - report.Delivered - report.Failed
		if report.Unfinished == 0 {
			break
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return report, errors.New("время ожидания доставки истекло")
		case <-timer.C:
		}
	}
	if report.Rejected > 0 || report.Failed > 0 {
		return report, errors.New("часть событий не принята или не доставлена")
	}
	return report, nil
}

func exchange(ctx context.Context, client *http.Client, method, address string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return errors.New("не удалось создать запрос")
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		// ошибка клиента может содержать адрес и пароль - выводим только причину без URL
		return errors.New("HTTP-запрос не выполнен")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(output)
}
