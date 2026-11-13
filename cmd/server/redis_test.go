package main

import (
	"testing"
)

func TestNewRedisClient(t *testing.T) {
	t.Run("invalid URL is an error", func(t *testing.T) {
		client, err := newRedisClient("not a url")
		if err == nil {
			_ = client.Close()
			t.Fatal("newRedisClient() error = nil, want error")
		}
		if client != nil {
			t.Errorf("client = %v, want nil on error", client)
		}
	})

	t.Run("an unreachable Redis still gives a usable client", func(t *testing.T) {
		// Nothing listens on port 1. The client must be returned anyway, so
		// that it can connect once Redis is up.
		client, err := newRedisClient("redis://127.0.0.1:1/0")
		if err != nil {
			t.Fatalf("newRedisClient() unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("client = nil, want a client")
		}
		t.Cleanup(func() { _ = client.Close() })

		if err := pingRedis(t.Context(), client); err == nil {
			t.Error("pingRedis() error = nil, want an error for an unreachable Redis")
		}
	})
}
