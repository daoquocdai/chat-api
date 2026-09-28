package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
	"github.com/gin-gonic/gin"
)

func TestWriteErrorReturnsRetryablePublishFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	errorWithContext := &model.EventPublishError{
		MessageID:   "11111111-1111-4111-8111-111111111111",
		ThreadID:    "22222222-2222-4222-8222-222222222222",
		SenderID:    "33333333-3333-4333-8333-333333333333",
		RecipientID: "44444444-4444-4444-8444-444444444444",
		Seq:         9,
		Cause:       errors.New("redis unavailable"),
	}

	writeError(context, errorWithContext)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["error"] != model.ErrEventPublishFailed.Error() {
		t.Fatalf("error = %q, want %q", response["error"], model.ErrEventPublishFailed.Error())
	}
	if strings.Contains(recorder.Body.String(), errorWithContext.MessageID) ||
		strings.Contains(recorder.Body.String(), errorWithContext.Cause.Error()) {
		t.Fatalf("response leaked internal publish context: %s", recorder.Body.String())
	}
}
