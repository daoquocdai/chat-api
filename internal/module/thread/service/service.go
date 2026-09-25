package service

import (
	"context"
	"strings"

	"github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

type Repository interface {
	CreateOrGetDirect(ctx context.Context, creatorID, peerID int64) (model.Thread, bool, error)
	ListByUser(ctx context.Context, userID int64) ([]model.Thread, error)
}

type UserFinder interface {
	GetByExternalID(ctx context.Context, externalID string) (usermodel.User, error)
}

type Service struct {
	repository Repository
	users      UserFinder
}

func New(repository Repository, users UserFinder) *Service {
	return &Service{repository: repository, users: users}
}

func (s *Service) CreateOrGetDirect(
	ctx context.Context,
	actorExternalID, peerExternalID string,
) (model.Thread, bool, error) {
	peerExternalID = strings.TrimSpace(peerExternalID)
	if peerExternalID == "" {
		return model.Thread{}, false, model.ErrPeerIDRequired
	}
	if actorExternalID == peerExternalID {
		return model.Thread{}, false, model.ErrSameUser
	}

	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return model.Thread{}, false, err
	}
	peer, err := s.users.GetByExternalID(ctx, peerExternalID)
	if err != nil {
		return model.Thread{}, false, err
	}
	if actor.ID == peer.ID {
		return model.Thread{}, false, model.ErrSameUser
	}

	return s.repository.CreateOrGetDirect(ctx, actor.ID, peer.ID)
}

func (s *Service) List(ctx context.Context, actorExternalID string) ([]model.Thread, error) {
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return nil, err
	}

	threads, err := s.repository.ListByUser(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if threads == nil {
		threads = []model.Thread{}
	}

	return threads, nil
}
