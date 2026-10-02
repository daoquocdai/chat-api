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
		WriteTimeout: cfg.Redis.PublishTimeout, ContextTimeoutEnabled: true,
	})
	defer redisClient.Close()

	hub := gateway.NewHub()
	tickets := wsticket.NewService(wsticket.NewRedisRepository(redisClient), cfg.WSTicketTTL)
	consumer := gateway.NewConsumer(redisClient, hub, cfg.Redis.Stream,
		gateway.DefaultGroup, gateway.DefaultConsumer, log.Default())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.Handle("GET /ws", gateway.NewHandler(hub, jwt, tickets, cfg.WSOrigins))
	server := &http.Server{Addr: cfg.WSAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("starting WebSocket gateway on %s", cfg.WSAddress)
	return serve(ctx, server, hub, consumer, redisClient)
}

// serve uses one shutdown deadline for HTTP, all sockets and the stream consumer.
// Listen/Serve errors take the same cleanup path as a termination signal.
func serve(ctx context.Context, server *http.Server, hub *gateway.Hub, consumer *gateway.Consumer, redisClient *redis.Client) error {
	consumerCtx, cancelConsumer := context.WithCancel(ctx)
	defer cancelConsumer()
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumer.Run(consumerCtx)
	}()
	serveDone := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveDone <- err
	}()
	var serveErr error
	serveReturned := false
	select {
	case <-ctx.Done():
	case serveErr = <-serveDone:
		serveReturned = true
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	hubDone := hub.Stop() // Reject upgrades/registrations before waiting on HTTP.
	cancelConsumer()
	shutdownDone := make(chan error, 1)
	go func() {
		err := server.Shutdown(shutdownCtx)
		<-consumerDone
		<-hubDone
		if !serveReturned {
			serveErr = <-serveDone
		}
		shutdownDone <- errors.Join(serveErr, err)
	}()
	select {
	case err := <-shutdownDone:
		log.Print("gateway shutdown complete: HTTP, consumer and WebSockets stopped")
		return err
	case <-shutdownCtx.Done():
		// Shutdown does not force active HTTP requests; closing Redis also releases
		// a consumer/authentication command still blocked on network I/O.
		log.Print("gateway shutdown deadline reached; forcing remaining I/O closed")
		_ = server.Close()
		_ = redisClient.Close()
		return errors.Join(shutdownCtx.Err(), <-shutdownDone)
	}
}
