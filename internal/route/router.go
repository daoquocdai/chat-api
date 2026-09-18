package route

import (
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message/handler"
	"github.com/gin-gonic/gin"
)

func New(messageHandler *handler.Handler) *gin.Engine {
	router := gin.Default()
	router.GET("/health", healthHandler)
	router.Any("/messages", gin.WrapF(messageHandler.Messages))
	return router
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
