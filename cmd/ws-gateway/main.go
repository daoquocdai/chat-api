package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/daoquocdai/chat-api/config"
	"github.com/daoquocdai/chat-api/internal/gateway"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/daoquocdai/chat-api/internal/wsticket"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.LoadGateway("config/config.yml")
	if err != nil {
		return fmt.Errorf("load gateway config: %w", err)
	}
	jwt, err := token.NewJWT(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL)
	if err != nil {
		return fmt.Errorf("create JWT manager: %w", err)
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Address, Password: cfg.Redis.Password, DB: cfg.Redis.Database,
		DialTimeout: cfg.Redis.PublishTimeout, ReadTimeout: 3 * time.Second,
		WriteTimeout: cfg.Redis.PublishTimeout,
	})
	defer redisClient.Close()

	hub := gateway.NewHub()
	tickets := wsticket.NewService(wsticket.NewRedisRepository(redisClient), cfg.WSTicketTTL)
	consumer := gateway.NewConsumer(redisClient, hub, cfg.Redis.Stream,
		gateway.DefaultGroup, gateway.DefaultConsumer, log.Default())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go consumer.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("GET /ws", gateway.NewHandler(hub, jwt, tickets, cfg.WSOrigins))
	server := &http.Server{Addr: cfg.WSAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("starting WebSocket gateway on %s", cfg.WSAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
