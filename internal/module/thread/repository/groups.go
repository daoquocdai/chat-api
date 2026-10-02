package repository

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	messagemodel "github.com/daoquocdai/chat-api/internal/module/message/model"
	"github.com/daoquocdai/chat-api/internal/module/thread/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *PostgresRepository) CreateGroup(ctx context.Context, creatorID int64, name string, memberIDs []int64, content string) (model.Thread, messagemodel.Message, error) {
	var thread model.Thread
	var message messagemodel.Message
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		created, err := q.CreateGroupThread(ctx, sqlc.CreateGroupThreadParams{Name: pgtype.Text{String: name, Valid: true}, CreatedBy: creatorID})
		if err != nil {
			return err
		}
		if err := q.CreateGroupParticipant(ctx, sqlc.CreateGroupParticipantParams{ThreadID: created.ID, UserID: creatorID, Role: "admin", JoinedSeq: 1}); err != nil {
			return err
		}
		for _, userID := range memberIDs {
			if err := q.CreateGroupParticipant(ctx, sqlc.CreateGroupParticipantParams{ThreadID: created.ID, UserID: userID, Role: "member", JoinedSeq: 1}); err != nil {
				return err
			}
		}
		message, err = createSystemMessage(ctx, q, created.ID, creatorID, content)
		if err != nil {
			return err
		}
		summary, err := q.GetThreadSummaryForUser(ctx, sqlc.GetThreadSummaryForUserParams{UserID: creatorID, ThreadExternalID: created.ExternalID})
		if err != nil {
			return err
		}
		thread = threadFromRow(summary)
		return nil
	})
	return thread, message, err
}

func (r *PostgresRepository) ChangeMember(ctx context.Context, threadExternalID string, actorID, targetID int64, action model.MembershipAction, content string) (messagemodel.Message, error) {
	id, err := parseThreadUUID(threadExternalID)
	if err != nil {
		return messagemodel.Message{}, err
	}
	var message messagemodel.Message
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.LockThreadByExternalID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrThreadNotFound
		}
		if err != nil {
			return err
		}
		if thread.Kind != "group" {
			return model.ErrNotGroup
		}
		actor, err := q.GetActiveParticipant(ctx, sqlc.GetActiveParticipantParams{ThreadID: thread.ID, UserID: actorID})
		if errors.Is(err, pgx.ErrNoRows) {
			// Repeating one's own leave is safe and republishes the same system message.
			if action == model.LeaveGroup && actorID == targetID {
				membership, lookupErr := q.GetLatestMembership(ctx, sqlc.GetLatestMembershipParams{ThreadID: thread.ID, UserID: actorID})
				if lookupErr == nil && membership.LeftSeq.Valid {
					message, err = systemMessageAt(ctx, q, thread.ID, membership.LeftSeq.Int64)
					return err
				}
				if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
					return lookupErr
				}
			}
			return model.ErrNotParticipant
		}
		if err != nil {
			return err
		}
		if action != model.LeaveGroup && actor.Role != "admin" {
			return model.ErrAdminRequired
		}
		if action == model.RemoveMember && actorID == targetID {
			return model.ErrUseLeave
		}

		membership, err := q.GetLatestMembership(ctx, sqlc.GetLatestMembershipParams{ThreadID: thread.ID, UserID: targetID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		exists := err == nil
		active := exists && !membership.LeftSeq.Valid
		if action == model.AddMember && active {
			message, err = systemMessageAt(ctx, q, thread.ID, membership.JoinedSeq)
			return err
		}
		if action != model.AddMember && !active {
			if !exists {
				return model.ErrMemberNotFound
			}
			message, err = systemMessageAt(ctx, q, thread.ID, membership.LeftSeq.Int64)
			return err
		}
		if action == model.AddMember {
			counts, err := q.CountActiveGroupMembers(ctx, thread.ID)
			if err != nil {
				return err
			}
			if counts.Members >= model.MaxGroupMembers {
				return model.ErrGroupTooLarge
			}
			if err := q.CreateGroupParticipant(ctx, sqlc.CreateGroupParticipantParams{ThreadID: thread.ID, UserID: targetID, Role: "member", JoinedSeq: thread.LastSeq + 1}); err != nil {
				return err
			}
		} else {
			if err := q.EndParticipant(ctx, sqlc.EndParticipantParams{ID: membership.ID, LeftSeq: pgtype.Int8{Int64: thread.LastSeq + 1, Valid: true}}); err != nil {
				return err
			}
			if action == model.LeaveGroup && actor.Role == "admin" {
				counts, err := q.CountActiveGroupMembers(ctx, thread.ID)
				if err != nil {
					return err
				}
				if counts.Members > 0 && counts.Admins == 0 {
					promoted, err := q.PromoteOldestGroupMember(ctx, thread.ID)
					if err != nil {
						return err
					}
					content += "; " + promoted + " trở thành quản trị viên"
				}
			}
		}
		message, err = createSystemMessage(ctx, q, thread.ID, actorID, content)
		return err
	})
	return message, err
}

func (r *PostgresRepository) ListMembers(ctx context.Context, threadExternalID string, actorID int64) ([]model.Member, error) {
	id, err := parseThreadUUID(threadExternalID)
	if err != nil {
		return nil, err
	}
	var members []model.Member
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.LockThreadByExternalID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrThreadNotFound
		}
		if err != nil {
			return err
		}
		if thread.Kind != "group" {
			return model.ErrNotGroup
		}
		if _, err := q.GetActiveParticipant(ctx, sqlc.GetActiveParticipantParams{ThreadID: thread.ID, UserID: actorID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrNotParticipant
			}
			return err
		}
		rows, err := q.ListGroupMembers(ctx, sqlc.ListGroupMembersParams{ThreadID: thread.ID, ActorID: actorID})
		if err != nil {
			return err
		}
		members = make([]model.Member, len(rows))
		for i, row := range rows {
			members[i] = model.Member{ExternalID: row.ExternalID.String(), Username: row.Username, Role: row.Role, JoinedSeq: row.JoinedSeq, LastReadSeq: row.LastReadSeq}
		}
		return nil
	})
	return members, err
}

func createSystemMessage(ctx context.Context, q *sqlc.Queries, threadID, actorID int64, content string) (messagemodel.Message, error) {
	seq, err := q.IncrementThreadSequence(ctx, threadID)
	if err != nil {
		return messagemodel.Message{}, err
	}
	row, err := q.CreateSystemMessage(ctx, sqlc.CreateSystemMessageParams{ThreadID: threadID, SenderID: actorID, Seq: seq, Content: content})
	if err != nil {
		return messagemodel.Message{}, err
	}
	return systemMessageFromRow(sqlc.GetThreadMessageAtSequenceRow(row)), nil
}

func systemMessageAt(ctx context.Context, q *sqlc.Queries, threadID, seq int64) (messagemodel.Message, error) {
	row, err := q.GetThreadMessageAtSequence(ctx, sqlc.GetThreadMessageAtSequenceParams{ThreadID: threadID, Seq: seq})
	if err != nil {
		return messagemodel.Message{}, err
	}
	return systemMessageFromRow(row), nil
}

func systemMessageFromRow(row sqlc.GetThreadMessageAtSequenceRow) messagemodel.Message {
	return messagemodel.Message{ID: row.ID, ExternalID: row.ExternalID.String(), ThreadExternalID: row.ThreadExternalID.String(), ThreadKind: row.ThreadKind, SenderExternalID: row.SenderExternalID.String(), Seq: row.Seq, Kind: row.Kind, ContentFormat: row.ContentFormat, Content: row.Content, CreatedAt: row.CreatedAt.Time}
}
