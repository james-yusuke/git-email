package scan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/james-yusuke/git-email/internal/model"
)

const (
	maxEvidencePerFinding = 5
	maxGitErrorBytes      = 64 << 10
)

type GitScanner struct {
	GitBinary   string
	TempRoot    string
	Token       string
	AskPassPath string
}

type aggregate struct {
	count    int
	sources  map[string]struct{}
	evidence []model.Evidence
}

func (s *GitScanner) Scan(ctx context.Context, repository model.Repository, matcher *Matcher) ([]model.Finding, error) {
	gitBinary := s.GitBinary
	if gitBinary == "" {
		gitBinary = "git"
	}
	if _, err := exec.LookPath(gitBinary); err != nil {
		return nil, fmt.Errorf("find git executable: %w", err)
	}

	tempDir, err := os.MkdirTemp(s.TempRoot, "git-email-")
	if err != nil {
		return nil, fmt.Errorf("create temporary mirror directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	mirrorPath := filepath.Join(tempDir, "repository.git")
	cloneCommand, err := s.cloneCommand(ctx, gitBinary, repository.CloneURL, mirrorPath)
	if err != nil {
		return nil, err
	}
	cloneError := &limitedBuffer{limit: maxGitErrorBytes}
	cloneCommand.Stderr = cloneError
	if err := cloneCommand.Run(); err != nil {
		message := strings.TrimSpace(redact(cloneError.String(), s.Token))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("mirror clone failed: %s", message)
	}

	aggregates := make(map[string]*aggregate)
	addMatch := func(email, kind, objectSHA, path string) {
		item := aggregates[email]
		if item == nil {
			item = &aggregate{sources: make(map[string]struct{})}
			aggregates[email] = item
		}
		item.count++
		item.sources[kind] = struct{}{}
		if len(item.evidence) < maxEvidencePerFinding {
			item.evidence = append(item.evidence, model.Evidence{Kind: kind, ObjectSHA: objectSHA, Path: path})
		}
	}

	if err := scanObjects(ctx, gitBinary, mirrorPath, matcher, addMatch); err != nil {
		return nil, err
	}

	findings := make([]model.Finding, 0, len(aggregates))
	for email, item := range aggregates {
		sources := make([]string, 0, len(item.sources))
		for source := range item.sources {
			sources = append(sources, source)
		}
		findings = append(findings, model.Finding{
			Repository:    repository.FullName,
			RepositoryURL: repository.HTMLURL,
			Visibility:    repository.Visibility(),
			Status:        repository.Status(),
			Email:         email,
			Sources:       sources,
			MatchCount:    item.count,
			Evidence:      item.evidence,
		})
	}
	return findings, nil
}

func (s *GitScanner) cloneCommand(ctx context.Context, gitBinary, cloneURL, destination string) (*exec.Cmd, error) {
	if cloneURL == "" {
		return nil, errors.New("repository has no clone URL")
	}
	if s.Token != "" {
		parsed, err := url.Parse(cloneURL)
		if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.User != nil {
			return nil, fmt.Errorf("refusing to send GITHUB_TOKEN to non-GitHub HTTPS clone URL %q", cloneURL)
		}
		if s.AskPassPath == "" {
			return nil, errors.New("askpass executable is required when GITHUB_TOKEN is set")
		}
	}

	command := exec.CommandContext(ctx, gitBinary, "-c", "credential.helper=", "clone", "--mirror", "--quiet", "--", cloneURL, destination)
	command.Env = sanitizedEnvironment()
	if s.Token != "" {
		command.Env = append(command.Env,
			"GIT_ASKPASS="+s.AskPassPath,
			"GIT_TERMINAL_PROMPT=0",
			"GIT_EMAIL_ASKPASS_MODE=1",
			"GIT_EMAIL_ASKPASS_TOKEN="+s.Token,
		)
	}
	return command, nil
}

func sanitizedEnvironment() []string {
	blocked := map[string]struct{}{
		"GITHUB_TOKEN":            {},
		"GH_TOKEN":                {},
		"GIT_ASKPASS":             {},
		"GIT_EMAIL_ASKPASS_MODE":  {},
		"GIT_EMAIL_ASKPASS_TOKEN": {},
	}
	environment := os.Environ()
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := blocked[key]; !skip {
			result = append(result, entry)
		}
	}
	return result
}

type matchAdder func(email, kind, objectSHA, path string)

func scanObjects(ctx context.Context, gitBinary, mirrorPath string, matcher *Matcher, add matchAdder) error {
	childContext, cancel := context.WithCancel(ctx)
	defer cancel()

	revCommand := exec.CommandContext(childContext, gitBinary, "-C", mirrorPath, "rev-list", "--objects", "--all")
	revCommand.Env = sanitizedEnvironment()
	revOutput, err := revCommand.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open git rev-list output: %w", err)
	}
	revError := &limitedBuffer{limit: maxGitErrorBytes}
	revCommand.Stderr = revError

	catCommand := exec.CommandContext(childContext, gitBinary, "-C", mirrorPath, "cat-file", "--batch")
	catCommand.Env = sanitizedEnvironment()
	catInput, err := catCommand.StdinPipe()
	if err != nil {
		return fmt.Errorf("open git cat-file input: %w", err)
	}
	catOutputPipe, err := catCommand.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open git cat-file output: %w", err)
	}
	catOutput := bufio.NewReaderSize(catOutputPipe, 64<<10)
	catError := &limitedBuffer{limit: maxGitErrorBytes}
	catCommand.Stderr = catError

	if err := revCommand.Start(); err != nil {
		return fmt.Errorf("start git rev-list: %w", err)
	}
	if err := catCommand.Start(); err != nil {
		cancel()
		_ = revCommand.Wait()
		return fmt.Errorf("start git cat-file: %w", err)
	}

	processingErr := processObjectStream(revOutput, catInput, catOutput, matcher, add)
	_ = catInput.Close()
	if processingErr != nil {
		cancel()
	}
	revWaitErr := revCommand.Wait()
	catWaitErr := catCommand.Wait()

	if processingErr != nil {
		return processingErr
	}
	if revWaitErr != nil {
		return fmt.Errorf("git rev-list failed: %s", commandErrorMessage(revWaitErr, revError.String()))
	}
	if catWaitErr != nil {
		return fmt.Errorf("git cat-file failed: %s", commandErrorMessage(catWaitErr, catError.String()))
	}
	return nil
}

func processObjectStream(revOutput io.Reader, catInput io.Writer, catOutput *bufio.Reader, matcher *Matcher, add matchAdder) error {
	objects := bufio.NewScanner(revOutput)
	objects.Buffer(make([]byte, 64<<10), 4<<20)
	for objects.Scan() {
		line := objects.Text()
		objectSHA, path, _ := strings.Cut(line, " ")
		if objectSHA == "" {
			continue
		}
		if _, err := fmt.Fprintln(catInput, objectSHA); err != nil {
			return fmt.Errorf("write git cat-file request: %w", err)
		}
		header, err := catOutput.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read git cat-file header for %s: %w", objectSHA, err)
		}
		headerFields := strings.Fields(header)
		if len(headerFields) == 2 && headerFields[1] == "missing" {
			return fmt.Errorf("git object %s is missing", objectSHA)
		}
		if len(headerFields) != 3 {
			return fmt.Errorf("unexpected git cat-file header %q", strings.TrimSpace(header))
		}
		size, err := strconv.ParseInt(headerFields[2], 10, 64)
		if err != nil || size < 0 {
			return fmt.Errorf("invalid git object size in header %q", strings.TrimSpace(header))
		}

		limited := &io.LimitedReader{R: catOutput, N: size}
		switch headerFields[1] {
		case "commit":
			if err := scanCommit(limited, objectSHA, matcher, add); err != nil {
				return err
			}
		case "blob":
			if err := matcher.FindReader(limited, func(email string) {
				add(email, "blob", objectSHA, path)
			}); err != nil {
				return fmt.Errorf("scan blob %s: %w", objectSHA, err)
			}
		}
		if _, err := io.Copy(io.Discard, limited); err != nil {
			return fmt.Errorf("consume git object %s: %w", objectSHA, err)
		}
		terminator, err := catOutput.ReadByte()
		if err != nil {
			return fmt.Errorf("read git object terminator for %s: %w", objectSHA, err)
		}
		if terminator != '\n' {
			return fmt.Errorf("unexpected git object terminator for %s", objectSHA)
		}
	}
	if err := objects.Err(); err != nil {
		return fmt.Errorf("read git rev-list output: %w", err)
	}
	return nil
}

func scanCommit(reader io.Reader, objectSHA string, matcher *Matcher, add matchAdder) error {
	headers := bufio.NewReader(reader)
	for {
		line, err := headers.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read commit %s: %w", objectSHA, err)
		}
		trimmed := strings.TrimSuffix(line, "\n")
		if trimmed == "" {
			if _, drainErr := io.Copy(io.Discard, headers); drainErr != nil {
				return fmt.Errorf("consume commit %s: %w", objectSHA, drainErr)
			}
			return nil
		}
		for _, field := range []struct {
			prefix string
			kind   string
		}{
			{prefix: "author ", kind: "commit_author"},
			{prefix: "committer ", kind: "commit_committer"},
		} {
			if strings.HasPrefix(trimmed, field.prefix) {
				if email := emailFromGitIdentity(strings.TrimPrefix(trimmed, field.prefix)); email != "" {
					if normalized, ok := matcher.Match(email); ok {
						add(normalized, field.kind, objectSHA, "")
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func emailFromGitIdentity(identity string) string {
	end := strings.LastIndex(identity, ">")
	if end < 0 {
		return ""
	}
	start := strings.LastIndex(identity[:end], "<")
	if start < 0 || start+1 >= end {
		return ""
	}
	return identity[start+1 : end]
}

func commandErrorMessage(commandErr error, stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr != "" {
		return stderr
	}
	return commandErr.Error()
}

func redact(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	return originalLength, nil
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}
