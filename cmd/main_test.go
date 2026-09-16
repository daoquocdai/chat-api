package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/message/dto"
	"github.com/daoquocdai/chat-api/internal/module/message/handler"
	"github.com/daoquocdai/chat-api/internal/module/message/repository"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
	"github.com/daoquocdai/chat-api/internal/route"
)

func newServer() http.Handler {
	messageRepository := repository.NewMemory()
	messageService := service.New(messageRepository)
	messageHandler := handler.New(messageService)
	return route.New(messageHandler)
}

func TestHealth(t *testing.T) {
	server := newServer()

	request := httptest.NewRequest(
		http.MethodGet,
		"/health",
		nil,
	)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}

	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf(
			"Content-Type = %q, want %q",
			response.Header().Get("Content-Type"),
			"application/json",
		)
	}

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf(
			"status body = %q, want %q",
			body["status"],
			"ok",
		)
	}
}

func TestCreateThenListMessages(t *testing.T) {
	server := newServer()

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/messages",
		strings.NewReader(
			`{"sender":"alice","receiver":"bob","content":"hello"}`,
		),
	)
	createRequest.Header.Set("Content-Type", "application/json")

	createResponse := httptest.NewRecorder()
	server.ServeHTTP(createResponse, createRequest)

	if createResponse.Code != http.StatusCreated {
		t.Fatalf(
			"POST status = %d, want %d",
			createResponse.Code,
			http.StatusCreated,
		)
	}

	var created dto.MessageResponse
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode POST response: %v", err)
	}

	if created.ID != 1 {
		t.Fatalf("created ID = %d, want 1", created.ID)
	}

	if created.Content != "hello" {
		t.Fatalf(
			"created content = %q, want %q",
			created.Content,
			"hello",
		)
	}

	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/messages",
		nil,
	)
	listResponse := httptest.NewRecorder()

	server.ServeHTTP(listResponse, listRequest)

	if listResponse.Code != http.StatusOK {
		t.Fatalf(
			"GET status = %d, want %d",
			listResponse.Code,
			http.StatusOK,
		)
	}

	var messages []dto.MessageResponse
	if err := json.NewDecoder(listResponse.Body).Decode(&messages); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf(
			"message count = %d, want 1",
			len(messages),
		)
	}

	if messages[0] != created {
		t.Fatalf(
			"listed message = %+v, want %+v",
			messages[0],
			created,
		)
	}
}
