package tasktest

import (
	"fmt"
	"testing"
	"time"

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

func timePtrEqual(a, b *time.Time) bool {
	if a != nil && b != nil {
		return a.Equal(*b)
	}
	return a == b
}

func checkUTC(t *testing.T, field string, got time.Time) (hasError bool) {
	t.Helper()

	if hasError = got.Location() != time.UTC; hasError {
		t.Errorf("%s location = %v, want UTC", field, got.Location())
	}
	return hasError
}

func CheckTask(t *testing.T, got task.Task, want task.Task) (hasError bool) {
	t.Helper()
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
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, want.CreatedAt)
		hasError = true
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, want.UpdatedAt)
		hasError = true
	}
	if !timePtrEqual(got.DueAt, want.DueAt) {
		t.Errorf("due_at = %v, want %v", ptrToStr(got.DueAt), ptrToStr(want.DueAt))
		hasError = true
	}

	if hasErr := checkUTC(t, "created_at", got.CreatedAt); hasErr {
		hasError = true
	}

	if hasErr := checkUTC(t, "updated_at", got.UpdatedAt); hasErr {
		hasError = true
	}

	if got.DueAt != nil {
		if hasErr := checkUTC(t, "due_at", *got.DueAt); hasErr {
			hasError = true
		}
	}

	return hasError
}

func CheckPatch(t *testing.T, got task.Patch, want task.Patch) (hasError bool) {
	t.Helper()
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
	if !timePtrEqual(got.DueAt, want.DueAt) {
		t.Errorf("due_at = %v, want %v", ptrToStr(got.DueAt), ptrToStr(want.DueAt))
		hasError = true
	}

	return hasError
}
