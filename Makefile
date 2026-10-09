DATABASE_URL ?= postgres://chat:chat@localhost:5432/chat_api?sslmode=disable

.PHONY: run gateway test build wasm up migrate migrate-status sqlc migrate-create

run:
	go run ./cmd

gateway:
	go run ./cmd/ws-gateway

test:
	go test ./...

build:
	go build ./...

# Runtime and binary must come from the same Go toolchain.
ifeq ($(OS),Windows_NT)
wasm: SHELL := cmd.exe
wasm: .SHELLFLAGS := /C
endif
wasm:
ifeq ($(OS),Windows_NT)
	powershell -NoProfile -Command "$$env:GOOS='js'; $$env:GOARCH='wasm'; go build -o web/e2ee.wasm ./cmd/e2ee-wasm; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; Copy-Item -LiteralPath (Join-Path (go env GOROOT) 'lib/wasm/wasm_exec.js') -Destination web/wasm_exec.js -ErrorAction Stop"
else
	GOOS=js GOARCH=wasm go build -o web/e2ee.wasm ./cmd/e2ee-wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js
endif

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
