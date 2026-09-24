package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"golang.org/x/crypto/bcrypt"
)

const (
	minimumPasswordCharacters = 8
	maximumPasswordBytes      = 72
)

type Repository interface {
	Create(ctx context.Context, username string) (model.User, error)
	GetByExternalID(ctx context.Context, externalID string) (model.User, error)
	List(ctx context.Context) ([]model.User, error)
}

type AuthRepository interface {
	CreateWithPassword(ctx context.Context, username, passwordHash string) (model.User, error)
	GetCredentialsByUsername(ctx context.Context, username string) (model.Credentials, error)
}

type TokenCreator interface {
	Create(userID string) (string, error)
}

type Service struct {
	repository     Repository
	authRepository AuthRepository
	tokens         TokenCreator
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func NewWithAuth(repository Repository, authRepository AuthRepository, tokens TokenCreator) *Service {
	return &Service{
		repository:     repository,
		authRepository: authRepository,
		tokens:         tokens,
	}
}

func (s *Service) Create(ctx context.Context, username string) (model.User, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil {
		return model.User{}, model.ErrInvalidUsername
	}

	return s.repository.Create(ctx, username)
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

func (s *Service) Register(ctx context.Context, username, password string) (model.User, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil {
		return model.User{}, err
	}
	if !validPassword(password) {
		return model.User{}, model.ErrInvalidPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, err
	}

	return s.authRepository.CreateWithPassword(ctx, username, string(passwordHash))
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	username, err := model.NormalizeUsername(username)
	if err != nil || !validPassword(password) {
		return "", model.ErrInvalidCredentials
	}

	credentials, err := s.authRepository.GetCredentialsByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			return "", model.ErrInvalidCredentials
		}

		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(credentials.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", model.ErrInvalidCredentials
		}

		return "", err
	}

	return s.tokens.Create(credentials.User.ExternalID)
}

func validPassword(password string) bool {
	return utf8.ValidString(password) &&
		utf8.RuneCountInString(password) >= minimumPasswordCharacters &&
		len(password) <= maximumPasswordBytes &&
		!strings.ContainsRune(password, '\x00')
}
