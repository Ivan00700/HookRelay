package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	config := options{}
	flag.StringVar(&config.baseURL, "base-url", "http://127.0.0.1:8080", "адрес API HookRelay")
	flag.StringVar(&config.endpointURL, "endpoint-url", "", "адрес получателя webhook")
	flag.StringVar(&config.secret, "secret", os.Getenv("WEBHOOK_SECRET"), "общий секрет получателя")
	flag.IntVar(&config.count, "count", 100, "количество событий от 1 до 10000")
	flag.IntVar(&config.workers, "concurrency", 4, "число параллельных запросов от 1 до 64")
	flag.DurationVar(&config.timeout, "timeout", 30*time.Second, "общее время приёма и ожидания доставки")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, config.timeout)
	defer cancel()
	report, err := run(ctx, &http.Client{Timeout: 5 * time.Second}, config)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, "не удалось вывести результат")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
