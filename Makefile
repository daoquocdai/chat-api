DATABASE_URL ?= postgres://chat:chat@localhost:5432/chat_api?sslmode=disable

.PHONY: run test build up migrate migrate-status sqlc migrate-create

run:
	go run ./cmd

test:
	go test ./...

build:
	go build ./...

up:
	docker compose up -d

migrate:
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-status:
	goose -dir db/migrations postgres "$(DATABASE_URL)" status

sqlc:
	sqlc generate

migrate-create:
	goose -dir db/migrations create $(NAME) sql