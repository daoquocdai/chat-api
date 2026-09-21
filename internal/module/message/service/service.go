package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

type Repository interface {
	Create(
		ctx context.Context,
		senderID int64,
		receiverID int64,
		content string,
	) (model.Message, error)

	ListBetween(
		ctx context.Context,
		userOneID int64,
		userTwoID int64,
	) ([]model.Message, error)
}

type UserFinder interface {
	GetByExternalID(
		ctx context.Context,
		externalID string,
	) (usermodel.User, error)
}

type Service struct {
	repository Repository
	users      UserFinder
}

func New(repository Repository, users UserFinder) *Service {
	return &Service{
		repository: repository,
		users:      users,
	}
}

func (s *Service) Create(
	ctx context.Context,
	senderExternalID string,
	receiverExternalID string,
	content string,
) (model.Message, error) {
	senderExternalID = strings.TrimSpace(senderExternalID)
	receiverExternalID = strings.TrimSpace(receiverExternalID)
	content = strings.TrimSpace(content)

	if senderExternalID == "" || receiverExternalID == "" {
		return model.Message{}, model.ErrUserIDsRequired
	}

	if senderExternalID == receiverExternalID {
		return model.Message{}, model.ErrSameUser
	}

	length := utf8.RuneCountInString(content)
	if length == 0 || length > 1000 {
		return model.Message{}, model.ErrInvalidContent
	}

	sender, receiver, err := s.findUsers(
		ctx,
		senderExternalID,
		receiverExternalID,
	)
	if err != nil {
		return model.Message{}, err
	}

	return s.repository.Create(ctx, sender.ID, receiver.ID, content)
}

func (s *Service) ListBetween(
	ctx context.Context,
	userExternalID string,
	peerExternalID string,
) ([]model.Message, error) {
	userExternalID = strings.TrimSpace(userExternalID)
	peerExternalID = strings.TrimSpace(peerExternalID)

	if userExternalID == "" || peerExternalID == "" {
		return nil, model.ErrUserIDsRequired
	}

	if userExternalID == peerExternalID {
		return nil, model.ErrSameUser
	}

	user, peer, err := s.findUsers(ctx, userExternalID, peerExternalID)
	if err != nil {
		return nil, err
	}

	return s.repository.ListBetween(ctx, user.ID, peer.ID)
}

func (s *Service) findUsers(
	ctx context.Context,
	firstExternalID string,
	secondExternalID string,
) (usermodel.User, usermodel.User, error) {
	first, err := s.users.GetByExternalID(ctx, firstExternalID)
	if err != nil {
		return usermodel.User{}, usermodel.User{}, err
	}

	second, err := s.users.GetByExternalID(ctx, secondExternalID)
	if err != nil {
		return usermodel.User{}, usermodel.User{}, err
	}

	if first.ID == second.ID {
		return usermodel.User{}, usermodel.User{}, model.ErrSameUser
	}

	return first, second, nil
}
