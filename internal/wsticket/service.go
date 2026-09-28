package wsticket

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidTicket = errors.New("invalid or expired WebSocket ticket")
var ErrInvalidUser = errors.New("invalid user ID")

type Repository interface {
	Store(ctx context.Context, ticket, userID string, ttl time.Duration) (bool, error)
	Consume(ctx context.Context, ticket string) (string, error)
}

type Service struct {
	repository Repository
	ttl        time.Duration
}

func NewService(repository Repository, ttl time.Duration) *Service {
	return &Service{repository: repository, ttl: ttl}
}

func (s *Service) Issue(ctx context.Context, userID string) (string, error) {
	parsed, err := uuid.Parse(userID)
	if err != nil {
		return "", ErrInvalidUser
	}
	for range 3 {
		var bytes [32]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return "", err
		}
		ticket := base64.RawURLEncoding.EncodeToString(bytes[:])
		stored, err := s.repository.Store(ctx, ticket, parsed.String(), s.ttl)
		if err != nil {
			return "", err
		}
		if stored {
			return ticket, nil
		}
	}
	return "", errors.New("could not allocate WebSocket ticket")
}

func (s *Service) Consume(ctx context.Context, ticket string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != ticket {
		return "", ErrInvalidTicket
	}
	userID, err := s.repository.Consume(ctx, ticket)
	if err != nil {
		return "", err
	}
	parsed, err := uuid.Parse(userID)
	if err != nil {
		return "", ErrInvalidTicket
	}
	return parsed.String(), nil
}
