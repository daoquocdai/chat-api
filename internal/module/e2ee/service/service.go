package service

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/google/uuid"
)

const maximumBackupCiphertextBytes = 1024

type Repository interface {
	Account(ctx context.Context, userID int64) (dto.AccountResponse, error)
	Claim(ctx context.Context, actorID, recipientID int64, threadExternalID string) (e2ee.Bundle, error)
	Epochs(ctx context.Context, userID int64, threadExternalID string) (dto.EpochPageResponse, error)
	CreateEpoch(ctx context.Context, actorID int64, actorExternalID, threadExternalID string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error)
	Backup(ctx context.Context, userID int64, threadExternalID, epochExternalID string, backup e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error)
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

func (s *Service) actor(ctx context.Context, externalID string) (usermodel.User, error) {
	if !canonicalUUID(externalID) {
		return usermodel.User{}, usermodel.ErrInvalidUserID
	}
	user, err := s.users.GetByExternalID(ctx, externalID)
	if errors.Is(err, usermodel.ErrUserNotFound) {
		return usermodel.User{}, usermodel.ErrInvalidUserID
	}
	return user, err
}

func (s *Service) Account(ctx context.Context, actorExternalID string) (dto.AccountResponse, error) {
	actor, err := s.actor(ctx, actorExternalID)
	if err != nil {
		return dto.AccountResponse{}, err
	}
	account, err := s.repository.Account(ctx, actor.ID)
	if err != nil {
		return dto.AccountResponse{}, err
	}
	return account, nil
}

func (s *Service) Claim(ctx context.Context, actorExternalID, recipientExternalID string, request dto.ClaimRequest) (dto.Bundle, error) {
	if !canonicalUUID(actorExternalID) {
		return dto.Bundle{}, usermodel.ErrInvalidUserID
	}
	if !canonicalUUID(recipientExternalID) || !canonicalUUID(request.ThreadID) {
		return dto.Bundle{}, model.ErrInvalidClaim
	}
	if actorExternalID == recipientExternalID {
		return dto.Bundle{}, model.ErrForbidden
	}
	actor, err := s.actor(ctx, actorExternalID)
	if err != nil {
		return dto.Bundle{}, err
	}
	recipient, err := s.users.GetByExternalID(ctx, recipientExternalID)
	if err != nil {
		return dto.Bundle{}, err
	}
	if actor.ID == recipient.ID {
		return dto.Bundle{}, model.ErrForbidden
	}
	bundle, err := s.repository.Claim(ctx, actor.ID, recipient.ID, request.ThreadID)
	if err != nil {
		return dto.Bundle{}, err
	}
	return bundle, nil
}

func (s *Service) Epochs(ctx context.Context, actorExternalID, threadExternalID string) (dto.EpochPageResponse, error) {
	if !canonicalUUID(actorExternalID) {
		return dto.EpochPageResponse{}, usermodel.ErrInvalidUserID
	}
	if !canonicalUUID(threadExternalID) {
		return dto.EpochPageResponse{}, model.ErrInvalidEpoch
	}
	actor, err := s.actor(ctx, actorExternalID)
	if err != nil {
		return dto.EpochPageResponse{}, err
	}
	page, err := s.repository.Epochs(ctx, actor.ID, threadExternalID)
	if err != nil {
		return dto.EpochPageResponse{}, err
	}
	if page.Epochs == nil {
		page.Epochs = []dto.EpochResponse{}
	}
	return page, nil
}

func (s *Service) CreateEpoch(ctx context.Context, actorExternalID, threadExternalID string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
	if !canonicalUUID(actorExternalID) {
		return dto.EpochResponse{}, false, usermodel.ErrInvalidUserID
	}
	if !canonicalUUID(threadExternalID) || !canonicalUUID(request.EpochID) ||
		(request.PreviousEpochID != nil && (!canonicalUUID(*request.PreviousEpochID) || *request.PreviousEpochID == request.EpochID)) {
		return dto.EpochResponse{}, false, model.ErrInvalidEpoch
	}
	header, err := e2ee.ParseEpochHeader(request.Bootstrap)
	if err != nil || header.ThreadID != threadExternalID || header.EpochID != request.EpochID ||
		header.SenderID != actorExternalID || e2ee.ValidateEncryptedRecord(request.KeyBackup, maximumBackupCiphertextBytes) != nil {
		return dto.EpochResponse{}, false, model.ErrInvalidEpoch
	}
	actor, err := s.actor(ctx, actorExternalID)
	if err != nil {
		return dto.EpochResponse{}, false, err
	}
	epoch, created, err := s.repository.CreateEpoch(ctx, actor.ID, actorExternalID, threadExternalID, request)
	if err != nil {
		return dto.EpochResponse{}, false, err
	}
	return epoch, created, nil
}

func (s *Service) Backup(ctx context.Context, actorExternalID, threadExternalID, epochExternalID string, backup e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
	if !canonicalUUID(actorExternalID) {
		return e2ee.EncryptedRecord{}, usermodel.ErrInvalidUserID
	}
	if !canonicalUUID(threadExternalID) || !canonicalUUID(epochExternalID) ||
		e2ee.ValidateEncryptedRecord(backup, maximumBackupCiphertextBytes) != nil {
		return e2ee.EncryptedRecord{}, model.ErrInvalidEpoch
	}
	actor, err := s.actor(ctx, actorExternalID)
	if err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	stored, err := s.repository.Backup(ctx, actor.ID, threadExternalID, epochExternalID, backup)
	if err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	return stored, nil
}

func canonicalUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}
