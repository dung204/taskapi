package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"uuid"

	goredis "github.com/redis/go-redis/v9"
)

type RevocationStore struct {
	client    *goredis.Client
	accessTTL time.Duration
}

func NewRevocationStore(client *goredis.Client, accessTTL time.Duration) *RevocationStore {
	return &RevocationStore{
		client:    client,
		accessTTL: accessTTL,
	}
}

func (s *RevocationStore) RevokeSession(ctx context.Context, sid uuid.UUID) error {
	if err := s.client.Set(ctx, sessionKey(sid), "1", s.accessTTL).Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *RevocationStore) RevokeUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	if err := s.client.Set(ctx, validAfterKey(userID), at.Unix(), s.accessTTL).Err(); err != nil {
		return fmt.Errorf("revoke user: %w", err)
	}
	return nil
}

func (s *RevocationStore) Check(ctx context.Context, sid, userID uuid.UUID, iat time.Time) (bool, error) {
	vals, err := s.client.MGet(ctx, sessionKey(sid), validAfterKey(userID)).Result()
	if err != nil {
		return false, fmt.Errorf("check revocation: %w", err)
	}

	if vals[0] != nil {
		return true, nil
	}

	if vals[1] != nil {
		str, ok := vals[1].(string)
		if !ok {
			return false, fmt.Errorf("check revocation: unexpected valid_after type")
		}
		validAfter, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return false, fmt.Errorf("check revocation: parse valid_after: %w", err)
		}
		if iat.Unix() < validAfter {
			return true, nil
		}
	}
	return false, nil
}

func sessionKey(sid uuid.UUID) string {
	return "revoked:sid" + sid.String()
}

func validAfterKey(userID uuid.UUID) string {
	return "user:" + userID.String() + ":valid_after"
}
