package route

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserHandler interface {
	Register(c *gin.Context)
	Login(c *gin.Context)
	List(c *gin.Context)
}

type ThreadHandler interface {
	CreateOrGetDirect(c *gin.Context)
	List(c *gin.Context)
	MarkRead(c *gin.Context)
}

type MessageHandler interface {
	Send(c *gin.Context)
	List(c *gin.Context)
}

func New(
	userHandler UserHandler,
	threadHandler ThreadHandler,
	messageHandler MessageHandler,
	authenticate gin.HandlerFunc,
) *gin.Engine {
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
	router.POST("/auth/register", userHandler.Register)
	router.POST("/auth/login", userHandler.Login)

	authenticated := router.Group("")
	authenticated.Use(authenticate)
	authenticated.GET("/users", userHandler.List)
	authenticated.POST("/threads/direct", threadHandler.CreateOrGetDirect)
	authenticated.GET("/threads", threadHandler.List)
	authenticated.PUT("/threads/:id/read", threadHandler.MarkRead)
	authenticated.POST("/threads/:id/messages", messageHandler.Send)
	authenticated.GET("/threads/:id/messages", messageHandler.List)

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
