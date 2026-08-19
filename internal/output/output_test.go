package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/james-yusuke/git-email/internal/model"
)

func TestWriteTextShowsRepositoryAndNoContentExcerpt(t *testing.T) {
	report := model.Report{
		Owner: "owner", Complete: true, RepositoriesDiscovered: 1, RepositoriesScanned: 1,
		Findings: []model.Finding{{
			Repository: "owner/repo", RepositoryURL: "https://github.com/owner/repo", Visibility: "public", Status: model.StatusExposed,
			Email: "person@example.com", Sources: []string{"blob"}, MatchCount: 1,
			Evidence: []model.Evidence{{Kind: "blob", ObjectSHA: "abc123", Path: "README.md"}},
		}},
	}
	report.Finalize()
	var buffer bytes.Buffer
	if err := WriteText(&buffer, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"EXPOSED https://github.com/owner/repo", "person@example.com", "path=README.md"} {
		if !strings.Contains(buffer.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, buffer.String())
		}
	}
}

func TestWriteJSONAndExitCodes(t *testing.T) {
	report := model.Report{Owner: "owner", Complete: true}
	report.Finalize()
	var buffer bytes.Buffer
	if err := WriteJSON(&buffer, report); err != nil {
		t.Fatal(err)
	}
	var decoded model.Report
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Findings == nil {
		t.Fatal("expected findings to be an empty JSON array")
	}
	if code := ExitCode(report); code != 0 {
		t.Fatalf("no-finding exit code = %d", code)
	}
	report.Findings = []model.Finding{{Email: "found@example.com"}}
	if code := ExitCode(report); code != 1 {
		t.Fatalf("finding exit code = %d", code)
	}
	report.Complete = false
	if code := ExitCode(report); code != 2 {
		t.Fatalf("incomplete exit code = %d", code)
	}
}

func TestWriteTextShowsRewriteResults(t *testing.T) {
	report := model.Report{
		Owner: "owner", Complete: true,
		Rewrites: []model.RewriteResult{{
			Repository: "owner/repo", RepositoryURL: "https://github.com/owner/repo",
			ReplacementEmail: "123+owner@users.noreply.github.com", UpdatedRefs: []string{"refs/heads/main"},
		}},
	}
	report.Finalize()
	var buffer bytes.Buffer
	if err := WriteText(&buffer, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"REWRITTEN https://github.com/owner/repo", "123+owner@users.noreply.github.com", "refs/heads/main"} {
		if !strings.Contains(buffer.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, buffer.String())
		}
	}
}
