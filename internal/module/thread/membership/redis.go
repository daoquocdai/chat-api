// Package membership caches immutable PostgreSQL membership snapshots, never permissions.
package membership

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const SnapshotTTL = 15 * time.Minute

type Redis struct {
	client redis.Cmdable
	prefix string
}

type snapshot struct {
	ThreadID  string   `json:"thread_id"`
	Version   int64    `json:"version"`
	MemberIDs []string `json:"member_ids"`
}

func NewRedis(client redis.Cmdable, stream string) *Redis {
	return &Redis{client: client, prefix: stream + ":membership:"}
}

func (r *Redis) key(threadID string, version int64) string {
	return fmt.Sprintf("%s%s:%d", r.prefix, threadID, version)
}

// A nil list is a miss; malformed or mismatched snapshots are errors and use the same fallback.
func (r *Redis) Get(ctx context.Context, threadID string, version int64) ([]string, error) {
	data, err := r.client.Get(ctx, r.key(threadID, version)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value snapshot
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value.ThreadID != threadID || value.Version != version || len(value.MemberIDs) == 0 {
		return nil, fmt.Errorf("invalid membership snapshot")
	}
	seen := make(map[string]bool, len(value.MemberIDs))
	for _, id := range value.MemberIDs {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id || seen[id] {
			return nil, fmt.Errorf("invalid membership snapshot member")
		}
		seen[id] = true
	}
	return value.MemberIDs, nil
}

func (r *Redis) Put(ctx context.Context, threadID string, version int64, members []string) error {
	data, err := json.Marshal(snapshot{ThreadID: threadID, Version: version, MemberIDs: members})
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(threadID, version), data, SnapshotTTL).Err()
}
