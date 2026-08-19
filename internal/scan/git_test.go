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

func TestGitScannerRewritePreservesFilesAndReplacesCommitEmails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	fixtureRoot := t.TempDir()
	working := filepath.Join(fixtureRoot, "working")
	remote := filepath.Join(fixtureRoot, "remote.git")
	runGit(t, "", "init", "-q", "-b", "main", working)
	runGit(t, working, "config", "user.name", "Original User")
	runGit(t, working, "config", "user.email", "Target@Example.com")
	if err := os.WriteFile(filepath.Join(working, "data.txt"), []byte("important file contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, working, "add", "data.txt")
	runGit(t, working, "commit", "-q", "-m", "target metadata")
	targetCommit := strings.TrimSpace(runGitOutput(t, working, "rev-parse", "HEAD"))
	originalIdentityAndDates := runGitOutput(t, working, "show", "-s", "--format=%an|%cn|%at|%ct", targetCommit)
	originalTree := strings.TrimSpace(runGitOutput(t, working, "show", "-s", "--format=%T", targetCommit))
	runGit(t, working, "tag", "-a", "-m", "release v1", "v1")

	runGit(t, working, "config", "user.email", "safe@example.net")
	if err := os.WriteFile(filepath.Join(working, "second.txt"), []byte("descendant remains\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, working, "add", "second.txt")
	runGit(t, working, "commit", "-q", "-m", "safe descendant")
	runGit(t, "", "clone", "-q", "--bare", working, remote)

	matcher, err := NewMatcher([]string{"target@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	temporaryMirrors := filepath.Join(fixtureRoot, "rewrite-mirrors")
	if err := os.Mkdir(temporaryMirrors, 0o700); err != nil {
		t.Fatal(err)
	}
	scanner := GitScanner{TempRoot: temporaryMirrors}
	result, err := scanner.Rewrite(context.Background(), model.Repository{
		FullName: "owner/repo", HTMLURL: "https://github.com/owner/repo", CloneURL: remote,
	}, matcher, "123+owner@users.noreply.github.com")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.UpdatedRefs, "refs/heads/main") || !slices.Contains(result.UpdatedRefs, "refs/tags/v1") {
		t.Fatalf("updated refs = %v", result.UpdatedRefs)
	}

	emails := runGitOutput(t, "", "--git-dir", remote, "log", "--all", "--format=%ae%n%ce")
	if strings.Contains(strings.ToLower(emails), "target@example.com") {
		t.Fatalf("target email remains in rewritten refs:\n%s", emails)
	}
	if !strings.Contains(emails, "123+owner@users.noreply.github.com") || !strings.Contains(emails, "safe@example.net") {
		t.Fatalf("expected replacement and safe emails:\n%s", emails)
	}
	contents := runGitOutput(t, "", "--git-dir", remote, "show", "main:data.txt")
	if contents != "important file contents\n" {
		t.Fatalf("file contents changed: %q", contents)
	}
	messages := runGitOutput(t, "", "--git-dir", remote, "log", "main", "--format=%s")
	if !strings.Contains(messages, "target metadata") || !strings.Contains(messages, "safe descendant") {
		t.Fatalf("commit messages changed:\n%s", messages)
	}
	rewrittenIdentityAndDates := runGitOutput(t, "", "--git-dir", remote, "log", "main", "--grep=^target metadata$", "--format=%an|%cn|%at|%ct")
	if rewrittenIdentityAndDates != originalIdentityAndDates {
		t.Fatalf("names or timestamps changed: before %q, after %q", originalIdentityAndDates, rewrittenIdentityAndDates)
	}
	rewrittenTree := strings.TrimSpace(runGitOutput(t, "", "--git-dir", remote, "log", "main", "--grep=^target metadata$", "--format=%T"))
	if rewrittenTree != originalTree {
		t.Fatalf("file tree changed: before %s, after %s", originalTree, rewrittenTree)
	}

	entries, err := os.ReadDir(temporaryMirrors)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary rewrite mirror was not cleaned up: %v", entries)
	}
}

func TestEnvFilterScriptSafelyHandlesEmailMetacharacters(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not installed")
	}
	target := "o'connor+$tag@example.com"
	script := envFilterScript([]string{target}, "123+owner@users.noreply.github.com") + `
printf '%s\n%s\n' "$GIT_AUTHOR_EMAIL" "$GIT_COMMITTER_EMAIL"
`
	command := exec.Command("sh", "-c", script)
	command.Env = append(sanitizedEnvironment(),
		"GIT_AUTHOR_EMAIL=O'CONNOR+$TAG@EXAMPLE.COM",
		"GIT_COMMITTER_EMAIL=safe@example.net",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run env filter: %v\n%s", err, output)
	}
	if got := string(output); got != "123+owner@users.noreply.github.com\nsafe@example.net\n" {
		t.Fatalf("env filter output = %q", got)
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

func runGitOutput(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = sanitizedEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
