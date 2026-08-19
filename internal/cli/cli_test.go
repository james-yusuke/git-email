package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestParseOwner(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "james-yusuke", want: "james-yusuke"},
		{input: "https://github.com/james-yusuke", want: "james-yusuke"},
		{input: "https://www.github.com/james-yusuke/", want: "james-yusuke"},
	}
	for _, test := range tests {
		got, err := ParseOwner(test.input)
		if err != nil {
			t.Fatalf("ParseOwner(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("ParseOwner(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestParseOwnerRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{
		"-owner", "owner-", "owner--name", "https://example.com/owner", "https://github.com/owner/repo", "https://github.com/owner?tab=repositories",
	} {
		if _, err := ParseOwner(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}

func TestRewriteCommitsRequiresExplicitEmailAndConfirmation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	tests := []struct {
		arguments []string
		message   string
	}{
		{arguments: []string{"scan", "--rewrite-commits", "owner"}, message: "requires at least one explicit --email"},
		{arguments: []string{"scan", "--rewrite-commits", "--email", "target@example.com", "owner"}, message: "requires --yes"},
		{arguments: []string{"scan", "--rewrite-commits", "--yes", "--email", "target@example.com", "--public-only", "owner"}, message: "cannot be used with --public-only"},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), test.arguments, &stdout, &stderr); code != 2 {
			t.Fatalf("Run(%v) exit code = %d", test.arguments, code)
		}
		if !strings.Contains(stderr.String(), test.message) {
			t.Fatalf("Run(%v) stderr = %q, want %q", test.arguments, stderr.String(), test.message)
		}
	}
}
