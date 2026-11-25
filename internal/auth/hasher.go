package auth

import "context"

// Hasher hashes passwords and checks them against stored hashes.
type Hasher interface {
	// Hash hashes password with a fresh random salt.
	//
	// Returns:
	//   - encodedHash - the hash with its salt and parameters in one string, to
	//     store and later pass to Verify.
	//
	// Returns the following errors:
	//   - the context error - if ctx ends while waiting for a free hashing slot.
	Hash(ctx context.Context, password string) (encodedHash string, err error)

	// Verify checks password against encodedHash in constant time.
	//
	// Returns:
	//   - ok - whether password matches encodedHash.
	//
	// Returns the following errors:
	//   - [ErrInvalidHash] - if encodedHash is not a valid encoded hash.
	//   - the context error - if ctx ends while waiting for a free hashing slot.
	Verify(ctx context.Context, password, encodedHash string) (ok bool, err error)
}
