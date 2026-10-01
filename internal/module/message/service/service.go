package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const maximumContentCharacters = 1000

type Repository interface {
	Send(
		ctx context.Context,
		threadExternalID string,
		senderID int64,
		messageID, content string,
	) (model.Message, bool, error)
	List(
		ctx context.Context,
		threadExternalID string,
		userID int64,
		beforeSeq *int64,
		limit int,
	) (model.Page, error)
}

type UserFinder interface {
	GetByExternalID(ctx context.Context, externalID string) (usermodel.User, error)
}

type Publisher interface {
	Publish(ctx context.Context, event messageevent.MessageCreated) error
}

type Service struct {
	repository     Repository
	users          UserFinder
	publisher      Publisher
	publishTimeout time.Duration
}

func New(
	repository Repository,
	users UserFinder,
	publisher Publisher,
	publishTimeout time.Duration,
) *Service {
	return &Service{
		repository:     repository,
		users:          users,
		publisher:      publisher,
		publishTimeout: publishTimeout,
	}
}

func (s *Service) Send(
	ctx context.Context,
	actorExternalID, threadExternalID, messageID, content string,
) (model.Message, bool, error) {
	threadExternalID = strings.TrimSpace(threadExternalID)
	if threadExternalID == "" {
		return model.Message{}, false, model.ErrThreadIDRequired
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return model.Message{}, false, model.ErrMessageIDRequired
	}
	content = strings.TrimSpace(content)
	if length := utf8.RuneCountInString(content); !utf8.ValidString(content) ||
		length == 0 || length > maximumContentCharacters || strings.ContainsRune(content, '\x00') {
		return model.Message{}, false, model.ErrInvalidContent
	}

	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return model.Message{}, false, err
	}

	message, created, err := s.repository.Send(ctx, threadExternalID, actor.ID, messageID, content)
	if err != nil {
		return model.Message{}, false, err
	}

	if err := PublishMessage(ctx, s.publisher, s.publishTimeout, message); err != nil {
		return message, created, err
	}

	return message, created, nil
}

func (s *Service) List(
	ctx context.Context,
	actorExternalID, threadExternalID string,
	beforeSeq *int64,
	limit int,
) (model.Page, error) {
	threadExternalID = strings.TrimSpace(threadExternalID)
	if threadExternalID == "" {
		return model.Page{}, model.ErrThreadIDRequired
	}
	if beforeSeq != nil && *beforeSeq <= 0 {
		return model.Page{}, model.ErrInvalidBeforeSeq
	}
	if limit < 1 || limit > model.MaximumPageLimit {
		return model.Page{}, model.ErrInvalidLimit
	}

	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return model.Page{}, err
	}

	page, err := s.repository.List(
		ctx,
		threadExternalID,
		actor.ID,
		beforeSeq,
		limit,
	)
	if err != nil {
		return model.Page{}, err
	}

	if page.Messages == nil {
		page.Messages = []model.Message{}
	}

	return page, nil
}
