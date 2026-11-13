package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// newTestHasher uses cheap parameters so the tests stay fast.
func newTestHasher(concurrency int) *Argon2Hasher {
	return &Argon2Hasher{
		time:    1,
		memory:  8,
		threads: 1,
		sem:     make(chan struct{}, concurrency),
	}
}

func TestHashVerify(t *testing.T) {
	h := newTestHasher(1)
	ctx := context.Background()

	tests := []struct {
		name          string
		password      string
		verifyAgainst string
		want          bool
	}{
		{name: "same password", password: "correct horse", verifyAgainst: "correct horse", want: true},
		{name: "wrong password", password: "correct horse", verifyAgainst: "battery staple", want: false},
		{name: "different case", password: "Password", verifyAgainst: "password", want: false},
		{name: "trailing space", password: "password", verifyAgainst: "password ", want: false},
		{name: "empty password", password: "", verifyAgainst: "", want: true},
		{name: "empty vs non-empty", password: "", verifyAgainst: "x", want: false},
		{name: "unicode", password: "pässwörd-密碼", verifyAgainst: "pässwörd-密碼", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := h.Hash(ctx, tt.password)
			if err != nil {
				t.Fatalf("Hash() unexpected error: %v", err)
			}

			got, err := h.Verify(ctx, tt.verifyAgainst, encoded)
			if err != nil {
				t.Fatalf("Verify() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Verify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHash_Format(t *testing.T) {
	h := &Argon2Hasher{time: 2, memory: 16, threads: 3, sem: make(chan struct{}, 1)}

	encoded, err := h.Hash(context.Background(), "password")
	if err != nil {
		t.Fatalf("Hash() unexpected error: %v", err)
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Fatalf("Hash() = %q, want 6 '$'-separated parts", encoded)
	}
	if parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		t.Errorf("Hash() prefix = %q, want $argon2id$v=19", strings.Join(parts[:3], "$"))
	}
	if want := "m=16,t=2,p=3"; parts[3] != want {
		t.Errorf("Hash() params = %q, want %q", parts[3], want)
	}
	if strings.ContainsAny(parts[4]+parts[5], "=") {
		t.Errorf("Hash() salt/hash must use unpadded base64, got %q %q", parts[4], parts[5])
	}
}

func TestHash_UniqueSalt(t *testing.T) {
	h := newTestHasher(1)
	ctx := context.Background()

	a, err := h.Hash(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("hashing the same password twice gave identical output %q", a)
	}
}

// A hash made with other parameters must still verify, since Verify reads
// the parameters from the encoded hash and not from the hasher.
func TestVerify_UsesParamsFromHash(t *testing.T) {
	ctx := context.Background()
	old := &Argon2Hasher{time: 2, memory: 16, threads: 2, sem: make(chan struct{}, 1)}

	encoded, err := old.Hash(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}

	got, err := newTestHasher(1).Verify(ctx, "password", encoded)
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}
	if !got {
		t.Error("Verify() = false, want true for a hash made with other parameters")
	}
}

func TestVerify_InvalidHash(t *testing.T) {
	h := newTestHasher(1)

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "not PHC", encoded: "plaintext"},
		{name: "too few parts", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA"},
		{name: "too many parts", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA$extra"},
		{name: "wrong algorithm", encoded: "$argon2i$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "bcrypt", encoded: "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ01234"},
		{name: "malformed version", encoded: "$argon2id$19$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "unsupported version", encoded: "$argon2id$v=16$m=8,t=1,p=1$c2FsdA$aGFzaA"},
		{name: "malformed params", encoded: "$argon2id$v=19$memory=8$c2FsdA$aGFzaA"},
		{name: "non-numeric params", encoded: "$argon2id$v=19$m=a,t=b,p=c$c2FsdA$aGFzaA"},
		{name: "zero iterations", encoded: "$argon2id$v=19$m=8,t=0,p=1$c2FsdA$aGFzaA"},
		{name: "zero threads", encoded: "$argon2id$v=19$m=8,t=1,p=0$c2FsdA$aGFzaA"},
		{name: "invalid salt base64", encoded: "$argon2id$v=19$m=8,t=1,p=1$!!!$aGFzaA"},
		{name: "invalid hash base64", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$!!!"},
		{name: "padded salt base64", encoded: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA==$aGFzaA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Verify(context.Background(), "password", tt.encoded)

			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("Verify() error = %v, want %v", err, ErrInvalidHash)
			}
			if got {
				t.Error("Verify() = true, want false on an invalid hash")
			}
		})
	}
}

func TestVerify_TamperedHash(t *testing.T) {
	h := newTestHasher(1)
	ctx := context.Background()

	encoded, err := h.Hash(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}

	// Flip the first character of the hash part.
	i := strings.LastIndex(encoded, "$") + 1
	flipped := "A"
	if encoded[i] == 'A' {
		flipped = "B"
	}
	tampered := encoded[:i] + flipped + encoded[i+1:]

	got, err := h.Verify(ctx, "password", tampered)
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}
	if got {
		t.Error("Verify() = true for a tampered hash, want false")
	}
}

func TestContext(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("canceled context is rejected before any work", func(t *testing.T) {
		h := newTestHasher(1)

		if _, err := h.Hash(canceled, "password"); !errors.Is(err, context.Canceled) {
			t.Errorf("Hash() error = %v, want %v", err, context.Canceled)
		}
		if _, err := h.Verify(canceled, "password", "x"); !errors.Is(err, context.Canceled) {
			t.Errorf("Verify() error = %v, want %v", err, context.Canceled)
		}
		if len(h.sem) != 0 {
			t.Errorf("semaphore holds %d slots, want 0", len(h.sem))
		}
	})

	t.Run("waiting for a slot honours the deadline", func(t *testing.T) {
		h := newTestHasher(1)
		h.sem <- struct{}{} // occupy the only slot

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		if _, err := h.Hash(ctx, "password"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Hash() error = %v, want %v", err, context.DeadlineExceeded)
		}
		if _, err := h.Verify(ctx, "password", "x"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Verify() error = %v, want %v", err, context.DeadlineExceeded)
		}
		if len(h.sem) != 1 {
			t.Errorf("semaphore holds %d slots, want 1 (the one we took)", len(h.sem))
		}
	})
}

func TestSemaphoreReleased(t *testing.T) {
	h := newTestHasher(1)
	ctx := context.Background()

	encoded, err := h.Hash(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.sem) != 0 {
		t.Errorf("slot not released after Hash: %d held", len(h.sem))
	}

	if _, err := h.Verify(ctx, "password", encoded); err != nil {
		t.Fatal(err)
	}
	if len(h.sem) != 0 {
		t.Errorf("slot not released after Verify: %d held", len(h.sem))
	}

	// An invalid hash returns early and must release the slot too.
	if _, err := h.Verify(ctx, "password", "garbage"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("Verify() error = %v, want %v", err, ErrInvalidHash)
	}
	if len(h.sem) != 0 {
		t.Errorf("slot not released after failed Verify: %d held", len(h.sem))
	}
}

func TestConcurrentUse(t *testing.T) {
	h := newTestHasher(2)
	ctx := context.Background()

	const n = 8
	errs := make(chan error, n)
	for i := range n {
		go func() {
			password := fmt.Sprintf("password-%d", i)

			encoded, err := h.Hash(ctx, password)
			if err != nil {
				errs <- err
				return
			}
			ok, err := h.Verify(ctx, password, encoded)
			if err == nil && !ok {
				err = fmt.Errorf("Verify(%q) = false, want true", password)
			}
			errs <- err
		}()
	}

	for range n {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
