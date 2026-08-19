package askpass

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRespondsToGitCredentialPrompts(t *testing.T) {
	t.Setenv("GIT_EMAIL_ASKPASS_TOKEN", "secret-token")
	tests := []struct {
		prompt string
		want   string
	}{
		{prompt: "Username for 'https://github.com':", want: "x-access-token"},
		{prompt: "Password for 'https://x-access-token@github.com':", want: "secret-token"},
	}
	for _, test := range tests {
		var output bytes.Buffer
		if err := Run([]string{test.prompt}, &output); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(output.String()); got != test.want {
			t.Fatalf("response to %q = %q, want %q", test.prompt, got, test.want)
		}
	}
}
