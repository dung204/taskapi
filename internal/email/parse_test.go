package email_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/email"
)

func TestParse(t *testing.T) {
	local := func(n int) string { return strings.Repeat("a", n) }
	// 254 characters is the longest accepted address.
	longest := local(254-len("@example.com")) + "@example.com"

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "an@example.com", "an@example.com", false},
		{"upper case", "An@Example.COM", "an@example.com", false},
		{"surrounding whitespace", " \tan@example.com\n", "an@example.com", false},
		{"plus tag", "an+tag@example.com", "an+tag@example.com", false},
		{"subdomain", "an@mail.example.co.uk", "an@mail.example.co.uk", false},
		{"254 characters", longest, longest, false},
		{"255 characters", "a" + longest, "", true},
		{"empty", "", "", true},
		{"only whitespace", "   ", "", true},
		{"no at sign", "an.example.com", "", true},
		{"no local part", "@example.com", "", true},
		{"no domain", "an@", "", true},
		{"no dot in the domain", "an@example", "", true},
		{"two at signs", "an@@example.com", "", true},
		{"inner space", "a n@example.com", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := email.Parse(tt.in)
			if tt.wantErr {
				if !errors.Is(err, email.ErrInvalidEmail) {
					t.Fatalf("error = %v, want ErrInvalidEmail", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Parse(%q) = (%q, %v), want %q", tt.in, got, err, tt.want)
			}
		})
	}
}
