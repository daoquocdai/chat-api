package service

import (
	"context"
	"encoding/base64"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/google/uuid"
)

type Repository interface {
	Upload(ctx context.Context, userID int64, request e2ee.UploadRequest) (e2ee.UploadResult, error)
	Claim(ctx context.Context, actorID, recipientID int64, threadExternalID string) (e2ee.Bundle, error)
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

func (s *Service) Upload(ctx context.Context, actorExternalID string, request e2ee.UploadRequest) (e2ee.UploadResult, error) {
	if !canonicalUUID(actorExternalID) {
		return e2ee.UploadResult{}, usermodel.ErrInvalidUserID
	}
	if err := validateUpload(actorExternalID, request); err != nil {
		return e2ee.UploadResult{}, err
	}
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return e2ee.UploadResult{}, err
	}
	result, err := s.repository.Upload(ctx, actor.ID, request)
	if err != nil {
		return e2ee.UploadResult{}, err
	}
	return result, nil
}

func (s *Service) Claim(ctx context.Context, actorExternalID, recipientExternalID string, request e2ee.ClaimRequest) (e2ee.Bundle, error) {
	if !canonicalUUID(actorExternalID) {
		return e2ee.Bundle{}, usermodel.ErrInvalidUserID
	}
	if !canonicalUUID(recipientExternalID) || !canonicalUUID(request.ThreadID) {
		return e2ee.Bundle{}, model.ErrInvalidClaim
	}
	if actorExternalID == recipientExternalID {
		return e2ee.Bundle{}, model.ErrForbidden
	}
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return e2ee.Bundle{}, err
	}
	recipient, err := s.users.GetByExternalID(ctx, recipientExternalID)
	if err != nil {
		return e2ee.Bundle{}, err
	}
	if actor.ID == recipient.ID {
		return e2ee.Bundle{}, model.ErrForbidden
	}
	bundle, err := s.repository.Claim(ctx, actor.ID, recipient.ID, request.ThreadID)
	if err != nil {
		return e2ee.Bundle{}, err
	}
	return bundle, nil
}

func canonicalUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func validateUpload(userID string, request e2ee.UploadRequest) error {
	if len(request.OneTimePrekeys) > 100 {
		return model.ErrInvalidPrekeys
	}
	// Verify the fixed SPK signature once, using the shared crypto profile.
	if err := e2ee.VerifyBundle(e2ee.Bundle{
		UserID: userID, IdentityPublicKey: request.IdentityPublicKey,
		SignedPrekey: request.SignedPrekey,
	}); err != nil {
		return model.ErrInvalidPrekeys
	}
	ids := map[int64]bool{request.SignedPrekey.KeyID: true}
	for _, opk := range request.OneTimePrekeys {
		if opk.KeyID <= 0 || ids[opk.KeyID] {
			return model.ErrInvalidPrekeys
		}
		ids[opk.KeyID] = true
		if len(opk.PublicKey) != base64.StdEncoding.EncodedLen(32) {
			return model.ErrInvalidPrekeys
		}
		public, err := base64.StdEncoding.Strict().DecodeString(opk.PublicKey)
		if err != nil || len(public) != 32 || base64.StdEncoding.EncodeToString(public) != opk.PublicKey {
			return model.ErrInvalidPrekeys
		}
		if err := e2ee.ValidatePublicKey([32]byte(public)); err != nil {
			return model.ErrInvalidPrekeys
		}
	}
	return nil
}
