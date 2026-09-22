package route

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserHandler interface {
	Create(c *gin.Context)
	GetByExternalID(c *gin.Context)
	List(c *gin.Context)
}

type MessageHandler interface {
	Create(c *gin.Context)
	ListBetween(c *gin.Context)
}

func New(userHandler UserHandler, messageHandler MessageHandler) *gin.Engine {
	router := gin.Default()

	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	router.HandleMethodNotAllowed = true
	router.NoMethod(noMethodHandler)

	router.GET("/health", healthHandler)
	router.GET("/", indexHandler)
	router.StaticFile("/app.js", "web/app.js")
	router.StaticFile("/style.css", "web/style.css")

	router.POST("/users", userHandler.Create)
	router.GET("/users", userHandler.List)
	router.GET("/users/:id", userHandler.GetByExternalID)
	router.POST("/messages", messageHandler.Create)
	router.GET("/messages", messageHandler.ListBetween)

	return router
}

func indexHandler(c *gin.Context) {
	c.File("web/index.html")
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func noMethodHandler(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
}
