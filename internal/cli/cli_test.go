package cli

import "testing"

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
