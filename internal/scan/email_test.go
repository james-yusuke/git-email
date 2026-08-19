package scan

import (
	"strings"
	"testing"
)

func TestMatcherAutoDetectionAndNoreplyExclusion(t *testing.T) {
	matcher, err := NewMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		email string
		want  bool
	}{
		{email: "Person+tag@Example.COM", want: true},
		{email: "12345+person@users.noreply.github.com", want: false},
		{email: "person@users.noreply.github.com", want: false},
		{email: "noreply@github.com", want: false},
		{email: "not-an-email", want: false},
	}
	for _, test := range tests {
		t.Run(test.email, func(t *testing.T) {
			_, got := matcher.Match(test.email)
			if got != test.want {
				t.Fatalf("Match(%q) = %v, want %v", test.email, got, test.want)
			}
		})
	}
}

func TestMatcherSpecifiedIsCaseInsensitiveAndAllowsNoreply(t *testing.T) {
	matcher, err := NewMatcher([]string{"Target@Example.com", "person@users.noreply.github.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"target@example.COM", "PERSON@users.noreply.github.com"} {
		normalized, ok := matcher.Match(email)
		if !ok {
			t.Fatalf("expected %q to match", email)
		}
		if normalized != strings.ToLower(email) {
			t.Fatalf("normalized email = %q", normalized)
		}
	}
	if _, ok := matcher.Match("other@example.com"); ok {
		t.Fatal("unexpected match for an unspecified email")
	}
}

func TestNewMatcherRejectsInvalidSpecifiedEmail(t *testing.T) {
	if _, err := NewMatcher([]string{"not-an-email"}); err == nil {
		t.Fatal("expected invalid email error")
	}
}

func TestFindReaderHandlesChunkBoundariesWithoutDuplicates(t *testing.T) {
	matcher, err := NewMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("x", chunkSize-7) + " "
	content := prefix + "cross.boundary@example.com then second@example.org"
	var found []string
	if err := matcher.FindReader(strings.NewReader(content), func(email string) {
		found = append(found, email)
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"cross.boundary@example.com", "second@example.org"}
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Fatalf("found %v, want %v", found, want)
	}
}
