package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message"
	"github.com/daoquocdai/chat-api/internal/module/message/dto"
)

type MessageService interface {
	Create(sender, receiver, content string) (message.Message, error)
	List() []message.Message
}

type Handler struct {
	service MessageService
}

func New(messageService MessageService) *Handler {
	return &Handler{service: messageService}
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		messages := h.service.List()
		writeJSON(w, http.StatusOK, dto.ToMessageResponses(messages))

	case http.MethodPost:
		var request dto.CreateMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		message, err := h.service.Create(
			request.Sender,
			request.Receiver,
			request.Content,
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, dto.ToMessageResponse(message))

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode JSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
