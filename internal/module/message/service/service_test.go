package service_test

import (
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/message/repository"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		name        string
		sender      string
		receiver    string
		content     string
		wantError   string
		wantSender  string
		wantContent string
	}{
		{
			name:        "valid message",
			sender:      " alice ",
			receiver:    "bob",
			content:     " hello ",
			wantSender:  "alice",
			wantContent: "hello",
		},
		{
			name:      "missing sender",
			sender:    " ",
			receiver:  "bob",
			content:   "hello",
			wantError: "sender is required",
		},
		{
			name:      "missing receiver",
			sender:    "alice",
			receiver:  " ",
			content:   "hello",
			wantError: "receiver is required",
		},
		{
			name:      "missing content",
			sender:    "alice",
			receiver:  "bob",
			content:   " ",
			wantError: "content is required",
		},
		{
			name:      "content too long",
			sender:    "alice",
			receiver:  "bob",
			content:   strings.Repeat("a", 101),
			wantError: "content is too long",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messageRepository := repository.NewMemory()
			messageService := service.New(messageRepository)

			message, err := messageService.Create(
				test.sender,
				test.receiver,
				test.content,
			)

			if test.wantError != "" {
				if err == nil {
					t.Fatalf(
						"error = nil, want %q",
						test.wantError,
					)
				}

				if err.Error() != test.wantError {
					t.Fatalf(
						"error = %q, want %q",
						err.Error(),
						test.wantError,
					)
				}

				if len(messageService.List()) != 0 {
					t.Fatal("invalid message was saved")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if message.ID != 1 {
				t.Fatalf("ID = %d, want 1", message.ID)
			}

			if message.Sender != test.wantSender {
				t.Fatalf(
					"sender = %q, want %q",
					message.Sender,
					test.wantSender,
				)
			}

			if message.Content != test.wantContent {
				t.Fatalf(
					"content = %q, want %q",
					message.Content,
					test.wantContent,
				)
			}

			if len(messageService.List()) != 1 {
				t.Fatal("valid message was not saved")
			}
		})
	}
}
