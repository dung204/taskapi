package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Hasher struct {
	time    uint32
	memory  uint32 // KiB
	threads uint8
	sem     chan struct{}
}

type Argon2HasherOpts struct {
	Time        uint32 // iterations, >= 1
	MemoryKiB   uint32 // >= 8*Threads
	Threads     uint8  // >= 1
	Concurrency int    // max simultaneous hashes, >= 1
}

func NewArgon2Hasher(opts Argon2HasherOpts) *Argon2Hasher {
	return &Argon2Hasher{
		time:    opts.Time,
		memory:  opts.MemoryKiB,
		threads: opts.Threads,
		sem:     make(chan struct{}, opts.Concurrency),
	}
}

func (h *Argon2Hasher) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Argon2Hasher) release() { <-h.sem }

func generateSalt() ([]byte, error) {
	saltLen := 16
	salt := make([]byte, saltLen)

	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

const PHCFormat = "$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s"

const keyLen = uint32(32)

// Hash implements [Hasher].
func (h *Argon2Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()

	salt, err := generateSalt()
	if err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, keyLen)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		PHCFormat,
		h.memory,
		h.time,
		h.threads,
		saltB64,
		hashB64,
	), nil
}

// Verify implements [Hasher].
func (h *Argon2Hasher) Verify(ctx context.Context, password, encodedHash string) (bool, error) {
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()

	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}

	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, ErrInvalidHash
	}

	if iterations < 1 || threads < 1 {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}

	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	actual := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expected)))

	return subtle.ConstantTimeCompare(expected, actual) == 1, nil
}
