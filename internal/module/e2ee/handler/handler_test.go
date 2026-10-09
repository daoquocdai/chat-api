package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/handler"
	"github.com/gin-gonic/gin"
)

const actorID = "11111111-1111-4111-8111-111111111111"

type fakeService struct {
	create func(context.Context, string, string, dto.CreateEpochRequest) (dto.EpochResponse, bool, error)
}

func (f fakeService) Account(context.Context, string) (dto.AccountResponse, error) {
	panic("unexpected account")
}
func (f fakeService) Claim(context.Context, string, string, dto.ClaimRequest) (dto.Bundle, error) {
	panic("unexpected claim")
}
func (f fakeService) Epochs(context.Context, string, string) (dto.EpochPageResponse, error) {
	panic("unexpected epochs")
}
func (f fakeService) Backup(context.Context, string, string, string, e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
	panic("unexpected backup")
}
func (f fakeService) CreateEpoch(ctx context.Context, actor, thread string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
	return f.create(ctx, actor, thread, request)
}

func epochRouter(service fakeService, actor string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.AuthenticatedUserIDKey, actor) })
	router.POST("/threads/:id/epochs", handler.New(service).CreateEpoch)
	return router
}

func perform(router *gin.Engine, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/threads/thread-id/epochs", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	return response
}

func TestEpochHTTPStatusesAndCanonicalWinner(t *testing.T) {
	const body = `{"epoch_id":"proposal","previous_epoch_id":null,"bootstrap":"opaque","key_backup":{"version":1,"nonce":"opaque","ciphertext":"opaque"}}`
	winner := dto.EpochResponse{EpochID: "committed", ThreadID: "thread-id", SenderID: actorID, Bootstrap: "committed-bootstrap"}
	for _, test := range []struct {
		name    string
		created bool
		err     error
		status  int
	}{
		{name: "new", created: true, status: http.StatusCreated},
		{name: "exact retry", status: http.StatusOK},
		{name: "concurrent winner", err: &dto.EpochConflictError{Epoch: winner}, status: http.StatusConflict},
		{name: "opaque database failure", err: errors.New("database bytea PRIVATE_SK"), status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := epochRouter(fakeService{create: func(_ context.Context, actor, thread string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
				if actor != actorID || thread != "thread-id" || request.PreviousEpochID != nil || request.Bootstrap != "opaque" {
					t.Fatal("HTTP request context changed")
				}
				return winner, test.created, test.err
			}}, actorID)
			response := perform(router, body)
			if response.Code != test.status || strings.Contains(response.Body.String(), "PRIVATE_SK") {
				t.Fatalf("status/error leaked: %d", response.Code)
			}
			if test.status == http.StatusConflict {
				var result struct {
					Epoch dto.EpochResponse `json:"epoch"`
				}
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Epoch.EpochID != winner.EpochID {
					t.Fatal("409 did not return the committed winner")
				}
			}
		})
	}
}

func TestEpochHTTPRejectsAmbiguousOrSecretFields(t *testing.T) {
	for _, body := range []string{
		`{"epoch_id":"one","epoch_id":"two","bootstrap":"x","key_backup":{}}`,
		`{"epoch_id":"one","bootstrap":"x","key_backup":{"version":1,"nonce":"x","ciphertext":"x","private_key":"SECRET"}}`,
		`{"epoch_id":"one","bootstrap":"x","key_backup":null}`,
		`{"epoch_id":"one","bootstrap":"x","key_backup":{"version":1,"nonce":"x","ciphertext":"x"},"session_key":"SECRET"}`,
		`{"epoch_id":"one","bootstrap":"x","key_backup":{"version":1,"nonce":"x","ciphertext":"x"}} {}`,
	} {
		response := perform(epochRouter(fakeService{}, actorID), body)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "SECRET") {
			t.Fatal("ambiguous or clear key data reached service")
		}
	}
	response := perform(epochRouter(fakeService{}, "invalid-actor"), `{}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("invalid actor was not rejected before reading the body")
	}
	response = perform(epochRouter(fakeService{}, actorID), strings.Repeat("x", 128*1024+1))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("oversized cryptographic body reached service")
	}
}
