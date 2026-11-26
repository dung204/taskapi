package postgres

import (
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

func runStoreBenchmark(b *testing.B, newStore func(tb testing.TB) task.Store) {
	b.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// The zero UUID is the legacy user that the migrations seed, so it satisfies
	// the foreign key on a real database without creating users.
	var owner uuid.UUID

	seed := func(tb testing.TB, s task.Store, n int) []uuid.UUID {
		tb.Helper()

		ids := make([]uuid.UUID, n)
		for i := range n {
			nt, err := task.New(task.NewParams{Title: "seed", OwnerID: owner, Status: task.StatusTodo, DueAt: nil}, now)
			if err != nil {
				tb.Fatalf("task.New: %v", err)
			}

			created, err := s.Create(tb.Context(), nt)
			if err != nil {
				tb.Fatalf("Create: %v", err)
			}

			ids[i] = created.ID
		}

		return ids
	}

	for _, n := range []int{10, 1000} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			s := newStore(b)
			ids := seed(b, s, n)

			b.Run("Get", func(b *testing.B) {
				id := ids[len(ids)/2]
				for b.Loop() {
					if _, err := s.Get(b.Context(), owner, id); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("List/first-page", func(b *testing.B) {
				f := task.ListFilter{Limit: 20, Offset: 0}
				for b.Loop() {
					if _, err := s.List(b.Context(), owner, f); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("List/deep-offset", func(b *testing.B) {
				f := task.ListFilter{Limit: 20, Offset: max(n-20, 0)}
				for b.Loop() {
					if _, err := s.List(b.Context(), owner, f); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
