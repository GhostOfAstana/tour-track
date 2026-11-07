package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"tour-track/internal/cache"
	"tour-track/internal/catalog"
	"tour-track/internal/seed"
	"tour-track/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("сервер остановлен", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}
	source, err := catalog.NewClient(cfg.apiURL, nil)
	if err != nil {
		return err
	}
	artists, capturedAt, err := seed.Load()
	if err != nil {
		return fmt.Errorf("резервный каталог: %w", err)
	}
	store := cache.New(source, artists, capturedAt)
	app, err := web.New(store, slog.Default())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go store.Run(ctx, 5*time.Minute)
	store.RequestRefresh()
	server := &http.Server{Addr: cfg.address, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", cfg.address)
	if err != nil {
		return fmt.Errorf("открыть порт HTTP: %w", err)
	}
	failures := make(chan error, 1)
	go func() { failures <- server.Serve(listener) }()
	slog.Info("Tour Track запущен", "address", listener.Addr().String(), "artists", len(artists))
	select {
	case err := <-failures:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("запуск HTTP: %w", err)
	case <-ctx.Done():
		// Новый контекст даёт текущим посетителям время закончить ответ после SIGTERM.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("остановка HTTP: %w", err)
		}
		return nil
	}
}

type config struct{ address, apiURL string }

func readConfig() (config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if port == "0" {
		return config{}, errors.New("PORT должен быть номером от 1 до 65535")
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return config{}, errors.New("PORT должен быть номером от 1 до 65535")
	}
	// HOST=127.0.0.1 подходит для локальной проверки; на хостинге нужен весь интерфейс.
	host := os.Getenv("HOST")
	apiURL := os.Getenv("API_BASE_URL")
	if apiURL == "" {
		apiURL = "https://groupietrackers.herokuapp.com/api"
	}
	return config{address: net.JoinHostPort(host, strconv.Itoa(parsedPort)), apiURL: apiURL}, nil
}
