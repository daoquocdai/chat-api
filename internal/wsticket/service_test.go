package wsticket

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryRepository struct {
	mu     sync.Mutex
	values map[string]string
}

func (r *memoryRepository) Store(_ context.Context, ticket, userID string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[ticket]; ok {
		return false, nil
	}
	r.values[ticket] = userID
	return true, nil
}

func (r *memoryRepository) Consume(_ context.Context, ticket string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	userID, ok := r.values[ticket]
	if !ok {
		return "", ErrInvalidTicket
	}
	delete(r.values, ticket)
	return userID, nil
}

func TestTicketIssueConsumeOnce(t *testing.T) {
	repository := &memoryRepository{values: make(map[string]string)}
	service := NewService(repository, 30*time.Second)
	userID := uuid.NewString()
	ticket, err := service.Issue(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket == userID || len(ticket) != 43 {
		t.Fatalf("ticket is not an opaque 32-byte token: %q", ticket)
	}
	got, err := service.Consume(context.Background(), ticket)
	if err != nil || got != userID {
		t.Fatalf("consume: %q %v", got, err)
	}
	if _, err := service.Consume(context.Background(), ticket); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("replayed ticket: %v", err)
	}
	if _, err := service.Consume(context.Background(), "not-a-ticket"); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("malformed ticket: %v", err)
	}
}

func TestIssueRejectsNonUUIDSubject(t *testing.T) {
	service := NewService(&memoryRepository{values: make(map[string]string)}, 30*time.Second)
	if _, err := service.Issue(context.Background(), "alice"); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("got %v", err)
	}
}
