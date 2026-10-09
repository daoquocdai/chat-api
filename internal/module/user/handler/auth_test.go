package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/user/handler"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

type noAuthCalls struct{}

func (noAuthCalls) Register(context.Context, string, string, e2ee.KDFProfile, e2ee.UploadRequest, e2ee.EncryptedRecord) (model.User, error) {
	panic("invalid auth request reached registration service")
}
func (noAuthCalls) Login(context.Context, string, string) (model.LoginResult, error) {
	panic("invalid auth request reached login service")
}
func (noAuthCalls) AuthParams(context.Context, string) (model.AuthParams, error) {
	panic("unexpected auth params call")
}
func (noAuthCalls) List(context.Context) ([]model.User, error) {
	panic("unexpected users call")
}

func TestAuthHandlersRejectRawPasswordAndAmbiguousCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := handler.New(noAuthCalls{})
	router.POST("/auth/register", h.Register)
	router.POST("/auth/login", h.Login)
	for _, test := range []struct{ path, body string }{
		{"/auth/register", `{"username":"alice","password":"raw-password"}`},
		{"/auth/register", `{"username":"alice","auth_credential":"credential","kdf":{},"public_bundle":{},"account_vault":{},"password":"raw-password"}`},
		{"/auth/login", `{"username":"alice","password":"raw-password"}`},
		{"/auth/login", `{"username":"alice","auth_credential":"credential","password":"raw-password"}`},
		{"/auth/login", `{"username":"alice","auth_credential":"one","auth_credential":"two"}`},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("path/status = %s/%d", test.path, response.Code)
		}
		if strings.Contains(response.Body.String(), "raw-password") || strings.Contains(response.Body.String(), "credential") {
			t.Fatal("auth input was echoed into response")
		}
	}
}
