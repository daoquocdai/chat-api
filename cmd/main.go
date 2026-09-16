package main

import (
	"log"
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message/handler"
	"github.com/daoquocdai/chat-api/internal/module/message/repository"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
	"github.com/daoquocdai/chat-api/internal/route"
)

func main() {
	messageRepository := repository.NewMemory()
	messageService := service.New(messageRepository)
	messageHandler := handler.New(messageService)

	log.Println("server is running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", route.New(messageHandler)))
}
