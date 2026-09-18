package main

import (
	"log"

	"github.com/daoquocdai/chat-api/internal/module/message/handler"
	"github.com/daoquocdai/chat-api/internal/module/message/repository"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
	"github.com/daoquocdai/chat-api/internal/route"
	"github.com/gin-gonic/gin"
)

func main() {
	messageRepository := repository.NewMemory()
	messageService := service.New(messageRepository)
	messageHandler := handler.New(messageService)

	gin.SetMode(gin.ReleaseMode)
	router := route.New(messageHandler)

	log.Println("server is running on http://localhost:8080")
	log.Fatal(router.Run(":8080"))
}
