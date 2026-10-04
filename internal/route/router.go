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
	CreateGroup(c *gin.Context)
	Members(c *gin.Context)
	AddMember(c *gin.Context)
	RemoveMember(c *gin.Context)
	Leave(c *gin.Context)
}

type MessageHandler interface {
	Send(c *gin.Context)
	List(c *gin.Context)
}

type WSTicketHandler interface {
	Issue(c *gin.Context)
}

type E2EEHandler interface {
	Upload(c *gin.Context)
	Claim(c *gin.Context)
}

func New(
	userHandler UserHandler,
	threadHandler ThreadHandler,
	messageHandler MessageHandler,
	wsTicketHandler WSTicketHandler,
	e2eeHandler E2EEHandler,
	authenticate gin.HandlerFunc,
) *gin.Engine {
	router := gin.Default()

	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	router.HandleMethodNotAllowed = true
	router.NoMethod(noMethodHandler)
	router.Use(webAssetCachePolicy)

	router.GET("/health", healthHandler)
	router.GET("/", indexHandler)
	router.StaticFile("/app.js", "web/app.js")
	router.StaticFile("/realtime-core.js", "web/realtime-core.js")
	router.StaticFile("/e2ee-wasm.js", "web/e2ee-wasm.js")
	router.StaticFile("/e2ee-state.js", "web/e2ee-state.js")
	router.StaticFile("/e2ee.js", "web/e2ee.js")
	router.StaticFile("/style.css", "web/style.css")
	router.GET("/e2ee.wasm", wasmHandler)
	router.StaticFile("/wasm_exec.js", "web/wasm_exec.js")
	router.POST("/auth/register", userHandler.Register)
	router.POST("/auth/login", userHandler.Login)

	authenticated := router.Group("")
	authenticated.Use(authenticate)
	authenticated.GET("/users", userHandler.List)
	authenticated.POST("/auth/ws-ticket", wsTicketHandler.Issue)
	authenticated.POST("/e2ee/prekeys", e2eeHandler.Upload)
	authenticated.POST("/e2ee/bundles/:user_id/claim", e2eeHandler.Claim)
	authenticated.POST("/threads/direct", threadHandler.CreateOrGetDirect)
	authenticated.POST("/threads/group", threadHandler.CreateGroup)
	authenticated.GET("/threads/:id/members", threadHandler.Members)
	authenticated.POST("/threads/:id/members", threadHandler.AddMember)
	authenticated.DELETE("/threads/:id/members/:user_id", threadHandler.RemoveMember)
	authenticated.POST("/threads/:id/leave", threadHandler.Leave)
	authenticated.GET("/threads", threadHandler.List)
	authenticated.PUT("/threads/:id/read", threadHandler.MarkRead)
	authenticated.POST("/threads/:id/messages", messageHandler.Send)
	authenticated.GET("/threads/:id/messages", messageHandler.List)

	return router
}

func webAssetCachePolicy(c *gin.Context) {
	switch c.Request.URL.Path {
	case "/", "/app.js", "/realtime-core.js", "/e2ee-wasm.js", "/e2ee-state.js", "/e2ee.js", "/style.css", "/e2ee.wasm", "/wasm_exec.js":
		c.Header("Cache-Control", "no-store")
	}
	c.Next()
}

func indexHandler(c *gin.Context) {
	c.File("web/index.html")
}

func wasmHandler(c *gin.Context) {
	c.Header("Content-Type", "application/wasm")
	c.File("web/e2ee.wasm")
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func noMethodHandler(c *gin.Context) {
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
}
