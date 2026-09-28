package wsticket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestTicketRESTRequiresBearer(t *testing.T) {
	jwt, err := token.NewJWT("test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(&memoryRepository{values: make(map[string]string)}, 30*time.Second)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/auth/ws-ticket", middleware.RequireAuthentication(jwt), NewHandler(service, "ws://localhost:8081/ws", time.Second).Issue)
	without := httptest.NewRecorder()
	router.ServeHTTP(without, httptest.NewRequest(http.MethodPost, "/auth/ws-ticket", nil))
	if without.Code != http.StatusUnauthorized {
		t.Fatalf("missing JWT: %d", without.Code)
	}
	userID := uuid.NewString()
	accessToken, err := jwt.Create(userID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/ws-ticket", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ticket response: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Ticket string `json:"ticket"`
		WSURL  string `json:"ws_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Ticket == "" || body.Ticket == accessToken || body.WSURL != "ws://localhost:8081/ws" {
		t.Fatalf("invalid ticket response: %+v", body)
	}
}
