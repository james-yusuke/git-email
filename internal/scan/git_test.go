package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/james-yusuke/git-email/internal/model"
)

func TestGitScannerScansAllRefsMetadataAndHistoricalBlobs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	fixtureRoot := t.TempDir()
	working := filepath.Join(fixtureRoot, "working")
	runGit(t, "", "init", "-q", "-b", "main", working)
	runGit(t, working, "config", "user.name", "Committer")
	runGit(t, working, "config", "user.email", "committer@example.com")

	historicalPath := filepath.Join(working, "historical.bin")
	if err := os.WriteFile(historicalPath, []byte("\x00private blob@example.com\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, working, "add", "historical.bin")
	runGitEnv(t, working, []string{
		"GIT_AUTHOR_NAME=Author",
		"GIT_AUTHOR_EMAIL=author@example.com",
	}, "commit", "-q", "-m", "initial")

	runGit(t, working, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(working, "feature.txt"), []byte("feature.only@example.net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, working, "add", "feature.txt")
	runGit(t, working, "commit", "-q", "-m", "feature")
	runGit(t, working, "tag", "v1")

	runGit(t, working, "checkout", "-q", "main")
	if err := os.Remove(historicalPath); err != nil {
		t.Fatal(err)
	}
	runGit(t, working, "add", "-u")
	runGit(t, working, "commit", "-q", "-m", "remove historical file")

	matcher, err := NewMatcher([]string{
		"author@example.com",
		"committer@example.com",
		"blob@example.com",
		"feature.only@example.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	temporaryMirrors := filepath.Join(fixtureRoot, "mirrors")
	if err := os.Mkdir(temporaryMirrors, 0o700); err != nil {
		t.Fatal(err)
	}
	scanner := GitScanner{TempRoot: temporaryMirrors}
	findings, err := scanner.Scan(context.Background(), model.Repository{
		FullName: "owner/repo", HTMLURL: "https://github.com/owner/repo", CloneURL: working,
	}, matcher)
	if err != nil {
		t.Fatal(err)
	}

	byEmail := make(map[string]model.Finding)
	for _, finding := range findings {
		byEmail[finding.Email] = finding
	}
	for _, email := range []string{"author@example.com", "committer@example.com", "blob@example.com", "feature.only@example.net"} {
		if _, ok := byEmail[email]; !ok {
			t.Fatalf("missing finding for %s; got %+v", email, findings)
		}
	}
	if !slices.Contains(byEmail["author@example.com"].Sources, "commit_author") {
		t.Fatalf("author sources = %v", byEmail["author@example.com"].Sources)
	}
	if !slices.Contains(byEmail["committer@example.com"].Sources, "commit_committer") {
		t.Fatalf("committer sources = %v", byEmail["committer@example.com"].Sources)
	}
	if !slices.Contains(byEmail["blob@example.com"].Sources, "blob") {
		t.Fatalf("blob sources = %v", byEmail["blob@example.com"].Sources)
	}

	entries, err := os.ReadDir(temporaryMirrors)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary mirror was not cleaned up: %v", entries)
	}
}

func TestCloneCommandNeverPlacesTokenInArgumentsOrURL(t *testing.T) {
	const token = "github_pat_super-secret"
	scanner := GitScanner{Token: token, AskPassPath: "/path/to/git-email"}
	command, err := scanner.cloneCommand(context.Background(), "git", "https://github.com/owner/repo.git", "/tmp/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(command.Args, " "), token) {
		t.Fatal("token was included in git command arguments")
	}
	if !strings.Contains(strings.Join(command.Env, "\n"), "GIT_EMAIL_ASKPASS_TOKEN="+token) {
		t.Fatal("token was not supplied through the askpass environment")
	}
	if got := redact("failure: "+token, token); strings.Contains(got, token) || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redact result = %q", got)
	}
}

func TestCloneCommandRefusesTokenForNonGitHubHost(t *testing.T) {
	scanner := GitScanner{Token: "secret", AskPassPath: "/path/to/git-email"}
	if _, err := scanner.cloneCommand(context.Background(), "git", "https://example.com/owner/repo.git", "/tmp/repo.git"); err == nil {
		t.Fatal("expected non-GitHub clone URL to be rejected")
	}
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	runGitEnv(t, directory, nil, arguments...)
}

func runGitEnv(t *testing.T, directory string, environment []string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(sanitizedEnvironment(), environment...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
