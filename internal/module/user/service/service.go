package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
)

type Repository interface {
	Create(
		ctx context.Context,
		username string,
	) (model.User, error)

	GetByExternalID(
		ctx context.Context,
		externalID string,
	) (model.User, error)

	List(ctx context.Context) ([]model.User, error)
}

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) Create(
	ctx context.Context,
	username string,
) (model.User, error) {
	username = strings.TrimSpace(username)
	username = strings.ToLower(username)

	length := utf8.RuneCountInString(username)
	if length == 0 || length > 50 || strings.ContainsRune(username, '\x00') {
		return model.User{}, model.ErrInvalidUsername
	}

	return s.repository.Create(ctx, username)
}

func (s *Service) GetByExternalID(
	ctx context.Context,
	externalID string,
) (model.User, error) {
	if externalID == "" {
		return model.User{}, model.ErrInvalidUserID
	}

	return s.repository.GetByExternalID(ctx, externalID)
}

func (s *Service) List(ctx context.Context) ([]model.User, error) {
	return s.repository.List(ctx)
}
