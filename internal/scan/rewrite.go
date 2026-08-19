package scan

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/james-yusuke/git-email/internal/model"
)

func (s *GitScanner) Rewrite(ctx context.Context, repository model.Repository, matcher *Matcher, replacementEmail string) (model.RewriteResult, error) {
	result := model.RewriteResult{
		Repository:       repository.FullName,
		RepositoryURL:    repository.HTMLURL,
		ReplacementEmail: NormalizeEmail(replacementEmail),
	}
	targets := matcher.Targets()
	if len(targets) == 0 {
		return result, fmt.Errorf("commit rewriting requires at least one explicitly specified email")
	}
	if !ValidEmail(result.ReplacementEmail) {
		return result, fmt.Errorf("invalid replacement email %q", replacementEmail)
	}

	gitBinary := s.GitBinary
	if gitBinary == "" {
		gitBinary = "git"
	}
	if _, err := exec.LookPath(gitBinary); err != nil {
		return result, fmt.Errorf("find git executable: %w", err)
	}
	tempDir, err := os.MkdirTemp(s.TempRoot, "git-email-rewrite-")
	if err != nil {
		return result, fmt.Errorf("create temporary rewrite directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	mirrorPath := filepath.Join(tempDir, "repository.git")
	cloneCommand, err := s.cloneCommand(ctx, gitBinary, repository.CloneURL, mirrorPath)
	if err != nil {
		return result, err
	}
	cloneError := &limitedBuffer{limit: maxGitErrorBytes}
	cloneCommand.Stderr = cloneError
	if err := cloneCommand.Run(); err != nil {
		return result, fmt.Errorf("mirror clone for rewrite failed: %s", commandErrorMessage(err, redact(cloneError.String(), s.Token)))
	}

	refsBefore, err := listRefs(ctx, gitBinary, mirrorPath, "refs/heads", "refs/tags")
	if err != nil {
		return result, err
	}
	affectedRefs := make([]string, 0, len(refsBefore))
	for ref := range refsBefore {
		matched, matchErr := refContainsTarget(ctx, gitBinary, mirrorPath, ref, matcher)
		if matchErr != nil {
			return result, matchErr
		}
		if matched {
			affectedRefs = append(affectedRefs, ref)
		}
	}
	sort.Strings(affectedRefs)
	if len(affectedRefs) == 0 {
		return result, nil
	}

	if err := runFilterBranch(ctx, gitBinary, mirrorPath, targets, result.ReplacementEmail, affectedRefs); err != nil {
		return result, err
	}
	if err := deleteBackupRefs(ctx, gitBinary, mirrorPath); err != nil {
		return result, err
	}
	if err := verifyCommitEmailsRemoved(ctx, gitBinary, mirrorPath, matcher); err != nil {
		return result, err
	}

	refsAfter, err := listRefs(ctx, gitBinary, mirrorPath, "refs/heads", "refs/tags")
	if err != nil {
		return result, err
	}
	for ref, oldSHA := range refsBefore {
		if newSHA, ok := refsAfter[ref]; ok && newSHA != oldSHA {
			result.UpdatedRefs = append(result.UpdatedRefs, ref)
		}
	}
	sort.Strings(result.UpdatedRefs)
	if len(result.UpdatedRefs) == 0 {
		return result, fmt.Errorf("matching commit metadata was found but no branch or tag changed")
	}

	if err := s.pushRewrittenRefs(ctx, gitBinary, mirrorPath, refsBefore, result.UpdatedRefs); err != nil {
		return result, err
	}
	return result, nil
}

func listRefs(ctx context.Context, gitBinary, repositoryPath string, prefixes ...string) (map[string]string, error) {
	arguments := []string{"-C", repositoryPath, "for-each-ref", "--format=%(refname)%00%(objectname)"}
	arguments = append(arguments, prefixes...)
	command := exec.CommandContext(ctx, gitBinary, arguments...)
	command.Env = sanitizedEnvironment()
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list Git refs: %w", err)
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		ref, sha, ok := strings.Cut(line, "\x00")
		if !ok || ref == "" || sha == "" {
			return nil, fmt.Errorf("unexpected Git ref record %q", line)
		}
		refs[ref] = sha
	}
	return refs, nil
}

func refContainsTarget(ctx context.Context, gitBinary, repositoryPath, ref string, matcher *Matcher) (bool, error) {
	command := exec.CommandContext(ctx, gitBinary, "-C", repositoryPath, "log", "--format=%ae%n%ce", ref)
	command.Env = sanitizedEnvironment()
	output, err := command.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("read commit metadata for %s: %w", ref, err)
	}
	commandError := &limitedBuffer{limit: maxGitErrorBytes}
	command.Stderr = commandError
	if err := command.Start(); err != nil {
		return false, fmt.Errorf("start commit metadata scan for %s: %w", ref, err)
	}
	found := false
	lines := bufio.NewScanner(output)
	for lines.Scan() {
		if _, ok := matcher.Match(lines.Text()); ok {
			found = true
			break
		}
	}
	if found {
		_ = output.Close()
	}
	waitErr := command.Wait()
	if lines.Err() != nil {
		return false, fmt.Errorf("scan commit metadata for %s: %w", ref, lines.Err())
	}
	if waitErr != nil && !found {
		return false, fmt.Errorf("scan commit metadata for %s: %s", ref, commandErrorMessage(waitErr, commandError.String()))
	}
	return found, nil
}

func runFilterBranch(ctx context.Context, gitBinary, repositoryPath string, targets []string, replacementEmail string, refs []string) error {
	arguments := []string{
		"-C", repositoryPath,
		"filter-branch", "--force",
		"--env-filter", envFilterScript(targets, replacementEmail),
		"--tag-name-filter", "cat",
		"--",
	}
	arguments = append(arguments, refs...)
	command := exec.CommandContext(ctx, gitBinary, arguments...)
	command.Env = append(sanitizedEnvironment(), "FILTER_BRANCH_SQUELCH_WARNING=1")
	commandOutput := &limitedBuffer{limit: maxGitErrorBytes}
	command.Stdout = commandOutput
	command.Stderr = commandOutput
	if err := command.Run(); err != nil {
		return fmt.Errorf("rewrite commit metadata: %s", commandErrorMessage(err, commandOutput.String()))
	}
	return nil
}

func envFilterScript(targets []string, replacementEmail string) string {
	patterns := make([]string, 0, len(targets))
	for _, target := range targets {
		patterns = append(patterns, shellSingleQuote(NormalizeEmail(target)))
	}
	joinedPatterns := strings.Join(patterns, "|")
	replacement := shellSingleQuote(NormalizeEmail(replacementEmail))
	return fmt.Sprintf(`
author_email_lower=$(printf '%%s' "$GIT_AUTHOR_EMAIL" | tr '[:upper:]' '[:lower:]')
case "$author_email_lower" in
  %s) GIT_AUTHOR_EMAIL=%s ;;
esac
committer_email_lower=$(printf '%%s' "$GIT_COMMITTER_EMAIL" | tr '[:upper:]' '[:lower:]')
case "$committer_email_lower" in
  %s) GIT_COMMITTER_EMAIL=%s ;;
esac
export GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL
`, joinedPatterns, replacement, joinedPatterns, replacement)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func deleteBackupRefs(ctx context.Context, gitBinary, repositoryPath string) error {
	refs, err := listRefs(ctx, gitBinary, repositoryPath, "refs/original")
	if err != nil {
		return err
	}
	for ref := range refs {
		command := exec.CommandContext(ctx, gitBinary, "-C", repositoryPath, "update-ref", "-d", ref)
		command.Env = sanitizedEnvironment()
		if output, commandErr := command.CombinedOutput(); commandErr != nil {
			return fmt.Errorf("remove local backup ref %s: %s", ref, commandErrorMessage(commandErr, string(output)))
		}
	}
	return nil
}

func verifyCommitEmailsRemoved(ctx context.Context, gitBinary, repositoryPath string, matcher *Matcher) error {
	refs, err := listRefs(ctx, gitBinary, repositoryPath, "refs/heads", "refs/tags")
	if err != nil {
		return err
	}
	for ref := range refs {
		matched, matchErr := refContainsTarget(ctx, gitBinary, repositoryPath, ref, matcher)
		if matchErr != nil {
			return matchErr
		}
		if matched {
			return fmt.Errorf("verification failed: target email remains reachable from %s", ref)
		}
	}
	return nil
}

func (s *GitScanner) pushRewrittenRefs(ctx context.Context, gitBinary, repositoryPath string, refsBefore map[string]string, changedRefs []string) error {
	arguments := []string{"-c", "credential.helper=", "-c", "remote.origin.mirror=false", "-C", repositoryPath, "push", "--atomic", "origin"}
	for _, ref := range changedRefs {
		arguments = append(arguments, "--force-with-lease="+ref+":"+refsBefore[ref])
	}
	for _, ref := range changedRefs {
		arguments = append(arguments, ref+":"+ref)
	}
	command := exec.CommandContext(ctx, gitBinary, arguments...)
	command.Env = s.authenticatedEnvironment()
	commandOutput := &limitedBuffer{limit: maxGitErrorBytes}
	command.Stdout = commandOutput
	command.Stderr = commandOutput
	if err := command.Run(); err != nil {
		return fmt.Errorf("force-push rewritten refs: %s", commandErrorMessage(err, redact(commandOutput.String(), s.Token)))
	}
	return nil
}
