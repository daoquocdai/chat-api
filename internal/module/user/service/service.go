package service

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"golang.org/x/crypto/bcrypt"
)

type Repository interface {
	CreateAccount(ctx context.Context, registration model.AccountRegistration) (model.User, error)
	GetAuthParamsByUsername(ctx context.Context, username string) (model.AuthParams, error)
	GetCredentialsByUsername(ctx context.Context, username string) (model.Credentials, error)
	GetByExternalID(ctx context.Context, externalID string) (model.User, error)
	List(ctx context.Context) ([]model.User, error)
}

type TokenCreator interface {
	Create(userID string) (string, error)
}

type Service struct {
	repository Repository
	tokens     TokenCreator
}

func New(repository Repository, tokens TokenCreator) *Service {
	return &Service{
		repository: repository,
		tokens:     tokens,
	}
}

func (s *Service) GetByExternalID(ctx context.Context, externalID string) (model.User, error) {
	if externalID == "" {
		return model.User{}, model.ErrInvalidUserID
	}

	return s.repository.GetByExternalID(ctx, externalID)
}

func (s *Service) List(ctx context.Context) ([]model.User, error) {
	return s.repository.List(ctx)
}

func (s *Service) Register(ctx context.Context, username, authCredential string, kdf e2ee.KDFProfile, publicBundle e2ee.UploadRequest, accountVault e2ee.EncryptedRecord) (model.User, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil {
		return model.User{}, err
	}
	if !validAuthCredential(authCredential) {
		return model.User{}, model.ErrInvalidAuthCredential
	}
	if e2ee.ValidateKDFProfile(kdf) != nil || e2ee.ValidatePublicBundle(publicBundle) != nil ||
		e2ee.ValidateEncryptedRecord(accountVault, e2ee.MaxVaultCiphertextBytes) != nil {
		return model.User{}, model.ErrInvalidAccountData
	}

	credentialHash, err := bcrypt.GenerateFromPassword([]byte(authCredential), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, err
	}

	return s.repository.CreateAccount(ctx, model.AccountRegistration{
		Username: username, AuthCredentialHash: string(credentialHash),
		KDF: kdf, PublicBundle: publicBundle, AccountVault: accountVault,
	})
}

func (s *Service) AuthParams(ctx context.Context, username string) (model.AuthParams, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil {
		return model.AuthParams{}, err
	}
	return s.repository.GetAuthParamsByUsername(ctx, username)
}

func (s *Service) Login(ctx context.Context, username, authCredential string) (model.LoginResult, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil || !validAuthCredential(authCredential) {
		return model.LoginResult{}, model.ErrInvalidCredentials
	}

	credentials, err := s.repository.GetCredentialsByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			return model.LoginResult{}, model.ErrInvalidCredentials
		}

		return model.LoginResult{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(credentials.AuthCredentialHash), []byte(authCredential)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return model.LoginResult{}, model.ErrInvalidCredentials
		}

		return model.LoginResult{}, err
	}

	accessToken, err := s.tokens.Create(credentials.User.ExternalID)
	if err != nil {
		return model.LoginResult{}, err
	}
	return model.LoginResult{AccessToken: accessToken, UserID: credentials.User.ExternalID}, nil
}

func validAuthCredential(value string) bool {
	if len(value) != base64.StdEncoding.EncodedLen(32) {
		return false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == value
}
