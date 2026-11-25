package opaquetoken

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	token, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	other, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if bytes.Equal(token, other) {
		t.Error("two generated tokens are equal")
	}

	got, err := Decode(Encode(token))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got, token) {
		t.Errorf("decoded = %x, want %x", got, token)
	}
}

func TestDecode_Invalid(t *testing.T) {
	tests := map[string]string{
		"empty":        "",
		"not base64":   "!!!",
		"too short":    Encode([]byte("short")),
		"too long":     Encode(bytes.Repeat([]byte{1}, 33)),
		"with padding": strings.Repeat("A", 43) + "=",
	}

	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(s); !errors.Is(err, ErrInvalid) {
				t.Errorf("err = %v, want %v", err, ErrInvalid)
			}
		})
	}
}

func TestHash(t *testing.T) {
	a, b := Hash([]byte("a")), Hash([]byte("a"))
	if !bytes.Equal(a, b) || len(a) != 32 {
		t.Errorf("Hash is not a stable 32-byte value: %x, %x", a, b)
	}
	if bytes.Equal(a, Hash([]byte("b"))) {
		t.Error("different tokens have the same hash")
	}
}
