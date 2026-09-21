package route

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserHandler interface {
	Create(c *gin.Context)
	GetByExternalID(c *gin.Context)
}

func New(userHandler UserHandler) *gin.Engine {
	router := gin.Default()

	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	router.HandleMethodNotAllowed = true
	router.NoMethod(noMethodHandler)

	router.GET("/health", healthHandler)
	router.POST("/users", userHandler.Create)
	router.GET("/users/:id", userHandler.GetByExternalID)

	return router
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func noMethodHandler(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{
		"error": "method not allowed",
	})
}
