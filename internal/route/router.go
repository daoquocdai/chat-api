package route

import (
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message/handler"
	"github.com/gin-gonic/gin"
)

func New(messageHandler *handler.Handler) *gin.Engine {
	router := gin.Default()
	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	router.HandleMethodNotAllowed = true
	router.NoMethod(noMethodHandler)
	router.GET("/health", healthHandler)
	router.GET("/messages", messageHandler.List)
	router.POST("/messages", messageHandler.Create)
	return router
}

func noMethodHandler(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
