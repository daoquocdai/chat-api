package service

import (
	"context"
	"strings"
	"unicode/utf8"

	messagemodel "github.com/daoquocdai/chat-api/internal/module/message/model"
	messageservice "github.com/daoquocdai/chat-api/internal/module/message/service"
	"github.com/daoquocdai/chat-api/internal/module/thread/model"
)

func (s *Service) CreateGroup(ctx context.Context, actorExternalID, name string, memberExternalIDs []string) (model.Thread, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 || strings.ContainsRune(name, '\x00') {
		return model.Thread{}, model.ErrInvalidGroupName
	}
	if len(memberExternalIDs) >= model.MaxGroupMembers {
		return model.Thread{}, model.ErrGroupTooLarge
	}
	// Validate the full list before touching persistence.
	memberExternalIDs = append([]string(nil), memberExternalIDs...)
	seen := make(map[string]bool, len(memberExternalIDs))
	for i, id := range memberExternalIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || seen[id] {
			return model.Thread{}, model.ErrInvalidMemberIDs
		}
		seen[id] = true
		memberExternalIDs[i] = id
	}
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return model.Thread{}, err
	}
	memberIDs := make([]int64, 0, len(memberExternalIDs))
	internalIDs := map[int64]bool{actor.ID: true}
	for _, externalID := range memberExternalIDs {
		member, err := s.users.GetByExternalID(ctx, externalID)
		if err != nil {
			return model.Thread{}, err
		}
		if internalIDs[member.ID] {
			return model.Thread{}, model.ErrInvalidMemberIDs
		}
		internalIDs[member.ID] = true
		memberIDs = append(memberIDs, member.ID)
	}
	thread, message, err := s.repository.CreateGroup(ctx, actor.ID, name, memberIDs, actor.Username+" đã tạo nhóm "+name)
	if err != nil {
		return model.Thread{}, err
	}
	if err := messageservice.PublishMessage(ctx, s.publisher, s.publishTimeout, message); err != nil {
		return thread, err
	}
	return thread, nil
}

func (s *Service) AddMember(ctx context.Context, actorExternalID, threadExternalID, targetExternalID string) (messagemodel.Message, error) {
	return s.changeMember(ctx, actorExternalID, threadExternalID, targetExternalID, model.AddMember)
}

func (s *Service) RemoveMember(ctx context.Context, actorExternalID, threadExternalID, targetExternalID string) (messagemodel.Message, error) {
	return s.changeMember(ctx, actorExternalID, threadExternalID, targetExternalID, model.RemoveMember)
}

func (s *Service) Leave(ctx context.Context, actorExternalID, threadExternalID string) (messagemodel.Message, error) {
	return s.changeMember(ctx, actorExternalID, threadExternalID, actorExternalID, model.LeaveGroup)
}

func (s *Service) changeMember(ctx context.Context, actorExternalID, threadExternalID, targetExternalID string, action model.MembershipAction) (messagemodel.Message, error) {
	threadExternalID = strings.TrimSpace(threadExternalID)
	if threadExternalID == "" {
		return messagemodel.Message{}, model.ErrThreadIDRequired
	}
	targetExternalID = strings.TrimSpace(targetExternalID)
	if targetExternalID == "" {
		return messagemodel.Message{}, model.ErrInvalidMemberIDs
	}
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return messagemodel.Message{}, err
	}
	target := actor
	if targetExternalID != actorExternalID {
		target, err = s.users.GetByExternalID(ctx, targetExternalID)
		if err != nil {
			return messagemodel.Message{}, err
		}
	}
	content := actor.Username + " đã rời nhóm"
	if action == model.AddMember {
		content = actor.Username + " đã thêm " + target.Username
	}
	if action == model.RemoveMember {
		content = actor.Username + " đã xóa " + target.Username + " khỏi nhóm"
	}
	message, err := s.repository.ChangeMember(ctx, threadExternalID, actor.ID, target.ID, action, content)
	if err != nil {
		return messagemodel.Message{}, err
	}
	if err := messageservice.PublishMessage(ctx, s.publisher, s.publishTimeout, message); err != nil {
		return message, err
	}
	return message, nil
}

func (s *Service) Members(ctx context.Context, actorExternalID, threadExternalID string) ([]model.Member, error) {
	threadExternalID = strings.TrimSpace(threadExternalID)
	if threadExternalID == "" {
		return nil, model.ErrThreadIDRequired
	}
	actor, err := s.users.GetByExternalID(ctx, actorExternalID)
	if err != nil {
		return nil, err
	}
	members, err := s.repository.ListMembers(ctx, threadExternalID, actor.ID)
	if err != nil {
		return nil, err
	}
	if members == nil {
		members = []model.Member{}
	}
	return members, nil
}
