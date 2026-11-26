// Package opaquetoken creates and parses the random tokens that are sent to a
// user (refresh tokens, links in emails) and kept on the server only as a
// SHA-256 hash.
package opaquetoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrInvalid is returned by [Decode] for a string that is not a token.
var ErrInvalid = errors.New("opaque token: invalid")

// Generate creates a new token from random bytes.
func Generate() ([]byte, error) {
	token := make([]byte, sha256.Size)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate opaque token: %w", err)
	}
	return token, nil
}

// Encode turns a token into the URL-safe string that is sent to the user.
func Encode(token []byte) string {
	return base64.RawURLEncoding.EncodeToString(token)
}

// Decode parses a string made by [Encode]. It returns [ErrInvalid] if s is not
// valid base64 or does not decode to a token of the right length.
func Decode(s string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != sha256.Size {
		return nil, ErrInvalid
	}
	return raw, nil
}

// Hash returns the SHA-256 hash of a token, which is what gets stored.
func Hash(token []byte) []byte {
	sum := sha256.Sum256(token)
	return sum[:]
}
