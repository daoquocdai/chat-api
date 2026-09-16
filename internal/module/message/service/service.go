package service

import (
	"errors"
	"strings"

	"github.com/daoquocdai/chat-api/internal/module/message"
)

type Repository interface {
	Create(newMessage message.Message) message.Message
	List() []message.Message
}

type Service struct {
	repository Repository
}

func New(messageRepository Repository) *Service {
	return &Service{repository: messageRepository}
}

func (s *Service) Create(sender, receiver, content string) (message.Message, error) {
	sender = strings.TrimSpace(sender)
	receiver = strings.TrimSpace(receiver)
	content = strings.TrimSpace(content)

	if err := validateCreateMessage(sender, receiver, content); err != nil {
		return message.Message{}, err
	}

	newMessage := message.Message{
		Sender:   sender,
		Receiver: receiver,
		Content:  content,
	}
	return s.repository.Create(newMessage), nil
}

func (s *Service) List() []message.Message {
	return s.repository.List()
}

func validateCreateMessage(sender, receiver, content string) error {
	if sender == "" {
		return errors.New("sender is required")
	}
	if receiver == "" {
		return errors.New("receiver is required")
	}
	if content == "" {
		return errors.New("content is required")
	}
	if len(content) > 100 {
		return errors.New("content is too long")
	}
	return nil
}
