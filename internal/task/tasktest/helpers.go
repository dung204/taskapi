package tasktest

import (
	"fmt"
	"testing"

	"github.com/dung204/taskapi/internal/task"
)

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func ptrToStr[T any](p *T) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

func CheckTask(t *testing.T, got task.Task, want task.Task) (hasError bool) {
	hasError = false

	if got.ID != want.ID {
		t.Errorf("id = %v, want %v", got.ID, want.ID)
		hasError = true
	}
	if got.Title != want.Title {
		t.Errorf("title = %q, want %q", got.Title, want.Title)
		hasError = true
	}
	if got.Description != want.Description {
		t.Errorf("description = %q, want %q", got.Description, want.Description)
		hasError = true
	}
	if got.Status != want.Status {
		t.Errorf("status = %q, want %q", got.Status, want.Status)
		hasError = true
	}
	if got.CreatedAt != want.CreatedAt {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, want.CreatedAt)
		hasError = true
	}
	if got.UpdatedAt != want.UpdatedAt {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, want.UpdatedAt)
		hasError = true
	}
	if !ptrEqual(got.DueAt, want.DueAt) {
		t.Errorf("due_at = %v, want %v", ptrToStr(got.DueAt), ptrToStr(want.DueAt))
		hasError = true
	}

	return hasError
}

func CheckPatch(t *testing.T, got task.Patch, want task.Patch) (hasError bool) {
	hasError = false

	if !ptrEqual(got.Title, want.Title) {
		t.Errorf("title = %q, want %q", ptrToStr(got.Title), ptrToStr(want.Title))
		hasError = true
	}
	if !ptrEqual(got.Description, want.Description) {
		t.Errorf("description = %q, want %q", ptrToStr(got.Description), ptrToStr(want.Description))
		hasError = true
	}
	if !ptrEqual(got.Status, want.Status) {
		t.Errorf("status = %q, want %q", ptrToStr(got.Status), ptrToStr(want.Status))
		hasError = true
	}
	if !ptrEqual(got.DueAt, want.DueAt) {
		t.Errorf("status = %v, want %v", ptrToStr(got.DueAt), ptrToStr(want.DueAt))
		hasError = true
	}

	return hasError
}
