package postgres

import (
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

// These tests need no database. pgx returns timestamptz values in time.Local,
// so each mapper is fed times in a fixed non-UTC zone and must hand back the
// same instants with the location set to time.UTC.

var (
	testZone = time.FixedZone("UTC+7", 7*60*60)
	testTime = time.Date(2026, 11, 19, 10, 30, 0, 0, testZone)
)

func assertUTC(t *testing.T, field string, got, want time.Time) {
	t.Helper()
	if got.Location() != time.UTC {
		t.Errorf("%s: location = %v, want UTC", field, got.Location())
	}
	if !got.Equal(want) {
		t.Errorf("%s: instant = %v, want %v", field, got, want)
	}
}

func assertUTCPtr(t *testing.T, field string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: got nil, want %v", field, want)
		return
	}
	assertUTC(t, field, *got, want)
}

func TestUTC(t *testing.T) {
	assertUTC(t, "utc", utc(testTime), testTime)
}

func TestUTCPtr(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := utcPtr(nil); got != nil {
			t.Errorf("utcPtr(nil) = %v, want nil", got)
		}
	})

	t.Run("value", func(t *testing.T) {
		in := testTime
		got := utcPtr(&in)
		assertUTCPtr(t, "utcPtr", got, testTime)
		if got == &in {
			t.Error("utcPtr returned its argument, want a new pointer")
		}
		if in.Location() != testZone {
			t.Errorf("utcPtr changed its argument's location to %v", in.Location())
		}
	})
}

func TestSessionFromRow(t *testing.T) {
	tm := testTime
	t.Run("with RotatedAt", func(t *testing.T) {
		s := sessionFromRow(sqlcgen.Session{ExpiresAt: testTime, CreatedAt: testTime, RotatedAt: &tm})
		assertUTC(t, "ExpiresAt", s.ExpiresAt, testTime)
		assertUTC(t, "CreatedAt", s.CreatedAt, testTime)
		assertUTCPtr(t, "RotatedAt", s.RotatedAt, testTime)
	})
	t.Run("nil RotatedAt", func(t *testing.T) {
		if s := sessionFromRow(sqlcgen.Session{ExpiresAt: testTime, CreatedAt: testTime}); s.RotatedAt != nil {
			t.Errorf("RotatedAt = %v, want nil", s.RotatedAt)
		}
	})
}

func TestTaskFromRow(t *testing.T) {
	tm := testTime
	t.Run("with DueAt", func(t *testing.T) {
		got := taskFromRow(sqlcgen.Task{CreatedAt: testTime, UpdatedAt: testTime, DueAt: &tm})
		assertUTC(t, "CreatedAt", got.CreatedAt, testTime)
		assertUTC(t, "UpdatedAt", got.UpdatedAt, testTime)
		assertUTCPtr(t, "DueAt", got.DueAt, testTime)
	})
	t.Run("nil DueAt", func(t *testing.T) {
		if got := taskFromRow(sqlcgen.Task{CreatedAt: testTime, UpdatedAt: testTime}); got.DueAt != nil {
			t.Errorf("DueAt = %v, want nil", got.DueAt)
		}
	})
}

func TestUserFromRow(t *testing.T) {
	tm := testTime
	t.Run("all set", func(t *testing.T) {
		u := userFromRow(sqlcgen.User{
			CreatedAt: testTime, UpdatedAt: testTime,
			EmailVerifiedAt: &tm, DisabledAt: &tm, TokensValidAfter: &tm,
		})
		assertUTC(t, "CreatedAt", u.CreatedAt, testTime)
		assertUTC(t, "UpdatedAt", u.UpdatedAt, testTime)
		assertUTCPtr(t, "EmailVerifiedAt", u.EmailVerifiedAt, testTime)
		assertUTCPtr(t, "DisabledAt", u.DisabledAt, testTime)
		assertUTCPtr(t, "TokensValidAfter", u.TokensValidAfter, testTime)
	})
	t.Run("nullable unset", func(t *testing.T) {
		u := userFromRow(sqlcgen.User{CreatedAt: testTime, UpdatedAt: testTime})
		if u.EmailVerifiedAt != nil || u.DisabledAt != nil || u.TokensValidAfter != nil {
			t.Errorf("nullable times = %v, %v, %v, want all nil", u.EmailVerifiedAt, u.DisabledAt, u.TokensValidAfter)
		}
	})
}

func TestInvitationFromRow(t *testing.T) {
	inv := invitationFromRow(sqlcgen.TaskInvitation{ExpiresAt: testTime, CreatedAt: testTime, UpdatedAt: testTime})
	assertUTC(t, "ExpiresAt", inv.ExpiresAt, testTime)
	assertUTC(t, "CreatedAt", inv.CreatedAt, testTime)
	assertUTC(t, "UpdatedAt", inv.UpdatedAt, testTime)
}

func TestInvitationMailFromRow(t *testing.T) {
	mail := invitationMailFromRow(sqlcgen.GetTaskInvitationForMailRow{
		TaskInvitation: sqlcgen.TaskInvitation{ExpiresAt: testTime, CreatedAt: testTime, UpdatedAt: testTime},
	})
	assertUTC(t, "ExpiresAt", mail.Invitation.ExpiresAt, testTime)
	assertUTC(t, "CreatedAt", mail.Invitation.CreatedAt, testTime)
	assertUTC(t, "UpdatedAt", mail.Invitation.UpdatedAt, testTime)
}

func TestMemberFromRow(t *testing.T) {
	m := memberFromRow(sqlcgen.TaskMember{CreatedAt: testTime, UpdatedAt: testTime})
	assertUTC(t, "CreatedAt", m.CreatedAt, testTime)
	assertUTC(t, "UpdatedAt", m.UpdatedAt, testTime)
}

func TestShareFromRow(t *testing.T) {
	tm := testTime
	t.Run("with ExpiresAt", func(t *testing.T) {
		s, err := shareFromRow(sqlcgen.ListTaskSharesRow{Kind: "invitation", CreatedAt: testTime, ExpiresAt: &tm})
		if err != nil {
			t.Fatalf("shareFromRow: %v", err)
		}
		assertUTC(t, "CreatedAt", s.CreatedAt, testTime)
		assertUTCPtr(t, "ExpiresAt", s.ExpiresAt, testTime)
	})
	t.Run("nil ExpiresAt", func(t *testing.T) {
		s, err := shareFromRow(sqlcgen.ListTaskSharesRow{Kind: "member", CreatedAt: testTime})
		if err != nil {
			t.Fatalf("shareFromRow: %v", err)
		}
		assertUTC(t, "CreatedAt", s.CreatedAt, testTime)
		if s.ExpiresAt != nil {
			t.Errorf("ExpiresAt = %v, want nil", s.ExpiresAt)
		}
	})
}
