package main

import (
	"context"
	"fmt"
	"log"

	"github.com/daoquocdai/chat-api/config"
	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	messagehandler "github.com/daoquocdai/chat-api/internal/module/message/handler"
	messagerepository "github.com/daoquocdai/chat-api/internal/module/message/repository"
	messageservice "github.com/daoquocdai/chat-api/internal/module/message/service"
	userhandler "github.com/daoquocdai/chat-api/internal/module/user/handler"
	userrepository "github.com/daoquocdai/chat-api/internal/module/user/repository"
	userservice "github.com/daoquocdai/chat-api/internal/module/user/service"
	"github.com/daoquocdai/chat-api/internal/route"
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

	userRepository := userrepository.New(queries)
	userService := userservice.New(userRepository)
	userHandler := userhandler.New(userService)

	messageRepository := messagerepository.New(queries)
	messageService := messageservice.New(messageRepository, userService)
	messageHandler := messagehandler.New(messageService)

	router := route.New(userHandler, messageHandler)

	log.Printf("starting HTTP server on %s", cfg.HTTPAddress)
	return router.Run(cfg.HTTPAddress)
}
