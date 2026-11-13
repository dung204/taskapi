package auth

import "context"

type Hasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(ctx context.Context, password, encodedHash string) (bool, error)
}
