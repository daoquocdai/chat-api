package repository

import (
	"sync"

	"github.com/daoquocdai/chat-api/internal/module/message"
)

type MemoryRepository struct {
	mu       sync.Mutex
	messages []message.Message
	nextID   int
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		messages: []message.Message{},
		nextID:   1,
	}
}

func (r *MemoryRepository) Create(newMessage message.Message) message.Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	newMessage.ID = r.nextID
	r.nextID++
	r.messages = append(r.messages, newMessage)
	return newMessage
}

func (r *MemoryRepository) List() []message.Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	messages := make([]message.Message, len(r.messages))
	copy(messages, r.messages)
	return messages
}
