package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/james-yusuke/git-email/internal/model"
	"github.com/james-yusuke/git-email/internal/scan"
)

type fakeSource struct {
	user         model.User
	repositories []model.Repository
	err          error
}

func (f fakeSource) AuthenticatedUser(context.Context) (model.User, error) {
	return f.user, f.err
}

func (f fakeSource) OwnedRepositories(context.Context) ([]model.Repository, error) {
	return f.repositories, f.err
}

func (f fakeSource) PublicRepositories(context.Context, string) ([]model.Repository, error) {
	return f.repositories, f.err
}

type fakeScanner struct {
	failRepository string
}

type fakeRewriter struct {
	replacement string
	calls       int
}

func (f *fakeRewriter) Rewrite(_ context.Context, repository model.Repository, _ *scan.Matcher, replacement string) (model.RewriteResult, error) {
	f.calls++
	f.replacement = replacement
	return model.RewriteResult{
		Repository: repository.FullName, RepositoryURL: repository.HTMLURL,
		ReplacementEmail: replacement, UpdatedRefs: []string{"refs/heads/main"},
	}, nil
}

func TestRunnerSkipsRewriteWhenScanIsIncomplete(t *testing.T) {
	matcher, _ := scan.NewMatcher([]string{"found@example.com"})
	rewriter := &fakeRewriter{}
	repositories := []model.Repository{{FullName: "owner/a"}, {FullName: "owner/b"}}
	runner := Runner{
		Source:  fakeSource{user: model.User{ID: 123, Login: "owner", PublicRepos: 2}, repositories: repositories},
		Scanner: fakeScanner{failRepository: "owner/b"}, Rewriter: rewriter,
	}
	report := runner.Run(context.Background(), Config{Owner: "owner", Matcher: matcher, RewriteCommits: true})
	if report.Complete || rewriter.calls != 0 {
		t.Fatalf("rewrite should be skipped after an incomplete scan: %+v", report)
	}
	foundSkip := false
	for _, reportError := range report.Errors {
		if reportError.Stage == "commit_rewrite" {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Fatalf("missing rewrite skip error: %+v", report.Errors)
	}
}

func (f fakeScanner) Scan(_ context.Context, repository model.Repository, _ *scan.Matcher) ([]model.Finding, error) {
	if repository.FullName == f.failRepository {
		return nil, errors.New("clone denied")
	}
	return []model.Finding{{
		Repository: repository.FullName, RepositoryURL: repository.HTMLURL,
		Visibility: repository.Visibility(), Status: repository.Status(), Email: "found@example.com", MatchCount: 1,
	}}, nil
}

func TestRunnerReportsIncompleteRepositoryPermissionsAndContinues(t *testing.T) {
	matcher, _ := scan.NewMatcher([]string{"found@example.com"})
	repositories := []model.Repository{{FullName: "owner/public", HTMLURL: "public", Private: false}}
	runner := Runner{
		Source: fakeSource{
			user:         model.User{Login: "owner", PublicRepos: 1, OwnedPrivateRepos: 1},
			repositories: repositories,
		},
		Scanner: fakeScanner{}, Jobs: 2,
	}
	report := runner.Run(context.Background(), Config{Owner: "owner", Matcher: matcher})
	if report.Complete {
		t.Fatal("expected incomplete report")
	}
	if report.RepositoriesScanned != 1 || len(report.Findings) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.Errors) != 1 || report.Errors[0].Stage != "repository_completeness" {
		t.Fatalf("unexpected errors: %+v", report.Errors)
	}
}

func TestRunnerRejectsDifferentAuthenticatedOwner(t *testing.T) {
	matcher, _ := scan.NewMatcher(nil)
	runner := Runner{Source: fakeSource{user: model.User{Login: "other"}}, Scanner: fakeScanner{}}
	report := runner.Run(context.Background(), Config{Owner: "owner", Matcher: matcher})
	if report.Complete || report.RepositoriesDiscovered != 0 || len(report.Errors) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunnerContinuesAfterRepositoryFailure(t *testing.T) {
	matcher, _ := scan.NewMatcher(nil)
	repositories := []model.Repository{{FullName: "owner/a"}, {FullName: "owner/b", Private: true}}
	runner := Runner{
		Source:  fakeSource{user: model.User{Login: "owner", PublicRepos: 1, OwnedPrivateRepos: 1}, repositories: repositories},
		Scanner: fakeScanner{failRepository: "owner/b"}, Jobs: 2,
	}
	report := runner.Run(context.Background(), Config{Owner: "owner", Matcher: matcher})
	if report.Complete || report.RepositoriesScanned != 1 || len(report.Errors) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunnerRewritesRepositoriesWithCommitFindings(t *testing.T) {
	matcher, _ := scan.NewMatcher([]string{"found@example.com"})
	repository := model.Repository{FullName: "owner/repo", HTMLURL: "https://github.com/owner/repo"}
	rewriter := &fakeRewriter{}
	runner := Runner{
		Source: fakeSource{
			user: model.User{ID: 123, Login: "owner", PublicRepos: 1}, repositories: []model.Repository{repository},
		},
		Scanner: fakeScannerWithSources{sources: []string{"commit_author"}}, Rewriter: rewriter,
	}
	report := runner.Run(context.Background(), Config{Owner: "owner", Matcher: matcher, RewriteCommits: true})
	if !report.Complete || len(report.Rewrites) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if rewriter.replacement != "123+owner@users.noreply.github.com" {
		t.Fatalf("replacement = %q", rewriter.replacement)
	}
}

type fakeScannerWithSources struct {
	sources []string
}

func (f fakeScannerWithSources) Scan(_ context.Context, repository model.Repository, _ *scan.Matcher) ([]model.Finding, error) {
	return []model.Finding{{
		Repository: repository.FullName, RepositoryURL: repository.HTMLURL, Visibility: repository.Visibility(),
		Status: repository.Status(), Email: "found@example.com", MatchCount: 1, Sources: f.sources,
	}}, nil
}
