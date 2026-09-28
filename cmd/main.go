package main

import (
	"context"
	"fmt"
	"log"

	"github.com/daoquocdai/chat-api/config"
	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	authmiddleware "github.com/daoquocdai/chat-api/internal/middleware"
	messagehandler "github.com/daoquocdai/chat-api/internal/module/message/handler"
	messagerepository "github.com/daoquocdai/chat-api/internal/module/message/repository"
	messageservice "github.com/daoquocdai/chat-api/internal/module/message/service"
	threadhandler "github.com/daoquocdai/chat-api/internal/module/thread/handler"
	threadrepository "github.com/daoquocdai/chat-api/internal/module/thread/repository"
	threadservice "github.com/daoquocdai/chat-api/internal/module/thread/service"
	userhandler "github.com/daoquocdai/chat-api/internal/module/user/handler"
	userrepository "github.com/daoquocdai/chat-api/internal/module/user/repository"
	userservice "github.com/daoquocdai/chat-api/internal/module/user/service"
	"github.com/daoquocdai/chat-api/internal/route"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load("config/config.yml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	queries := sqlc.New(pool)
	jwtManager, err := token.NewJWT(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL)
	if err != nil {
		return fmt.Errorf("create JWT manager: %w", err)
	}

	userRepository := userrepository.New(queries)
	userService := userservice.New(userRepository, jwtManager)
	userHandler := userhandler.New(userService)

	threadRepository := threadrepository.New(pool)
	threadService := threadservice.New(threadRepository, userService)
	threadHandler := threadhandler.New(threadService)

	messageRepository := messagerepository.New(pool)
	messageService := messageservice.New(messageRepository, userService)
	messageHandler := messagehandler.New(messageService)

	router := route.New(
		userHandler,
		threadHandler,
		messageHandler,
		authmiddleware.RequireAuthentication(jwtManager),
	)

	log.Printf("starting HTTP server on %s", cfg.HTTPAddress)
	return router.Run(cfg.HTTPAddress)
}
