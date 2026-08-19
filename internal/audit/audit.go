package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/james-yusuke/git-email/internal/model"
	"github.com/james-yusuke/git-email/internal/scan"
)

type RepositorySource interface {
	AuthenticatedUser(context.Context) (model.User, error)
	OwnedRepositories(context.Context) ([]model.Repository, error)
	PublicRepositories(context.Context, string) ([]model.Repository, error)
}

type RepositoryScanner interface {
	Scan(context.Context, model.Repository, *scan.Matcher) ([]model.Finding, error)
}

type RepositoryRewriter interface {
	Rewrite(context.Context, model.Repository, *scan.Matcher, string) (model.RewriteResult, error)
}

type Runner struct {
	Source   RepositorySource
	Scanner  RepositoryScanner
	Rewriter RepositoryRewriter
	Jobs     int
}

type Config struct {
	Owner          string
	PublicOnly     bool
	Matcher        *scan.Matcher
	RewriteCommits bool
}

func (r *Runner) Run(ctx context.Context, config Config) model.Report {
	report := model.Report{Owner: config.Owner, Complete: true}
	if r.Source == nil || r.Scanner == nil || config.Matcher == nil {
		report.Complete = false
		report.Errors = append(report.Errors, model.ReportError{Stage: "configuration", Message: "audit runner is not fully configured"})
		report.Finalize()
		return report
	}

	var repositories []model.Repository
	var authenticatedUser model.User
	if config.PublicOnly {
		var err error
		repositories, err = r.Source.PublicRepositories(ctx, config.Owner)
		if err != nil {
			addGlobalError(&report, "repository_discovery", err)
			report.Finalize()
			return report
		}
	} else {
		user, err := r.Source.AuthenticatedUser(ctx)
		if err != nil {
			addGlobalError(&report, "authentication", err)
			report.Finalize()
			return report
		}
		if !strings.EqualFold(user.Login, config.Owner) {
			addGlobalError(&report, "authentication", fmt.Errorf("authenticated GitHub user %q does not match requested owner %q", user.Login, config.Owner))
			report.Finalize()
			return report
		}
		authenticatedUser = user
		repositories, err = r.Source.OwnedRepositories(ctx)
		if err != nil {
			addGlobalError(&report, "repository_discovery", err)
			report.Finalize()
			return report
		}
		publicCount, privateCount := repositoryCounts(repositories)
		if publicCount != user.PublicRepos || privateCount != user.OwnedPrivateRepos {
			addGlobalError(&report, "repository_completeness", fmt.Errorf(
				"token can list %d public and %d private owned repositories; account reports %d public and %d private (grant the token access to all repositories)",
				publicCount, privateCount, user.PublicRepos, user.OwnedPrivateRepos,
			))
		}
	}

	report.RepositoriesDiscovered = len(repositories)
	if len(repositories) == 0 {
		report.Finalize()
		return report
	}

	type scanResult struct {
		repository model.Repository
		findings   []model.Finding
		err        error
	}
	jobs := r.Jobs
	if jobs <= 0 {
		jobs = 4
	}
	if jobs > len(repositories) {
		jobs = len(repositories)
	}
	queue := make(chan model.Repository, len(repositories))
	results := make(chan scanResult, len(repositories))
	for _, repository := range repositories {
		queue <- repository
	}
	close(queue)
	for range jobs {
		go func() {
			for repository := range queue {
				findings, err := r.Scanner.Scan(ctx, repository, config.Matcher)
				results <- scanResult{repository: repository, findings: findings, err: err}
			}
		}()
	}

	for range repositories {
		result := <-results
		if result.err != nil {
			report.Complete = false
			report.Errors = append(report.Errors, model.ReportError{
				Repository: result.repository.FullName,
				Stage:      "repository_scan",
				Message:    result.err.Error(),
			})
			continue
		}
		report.RepositoriesScanned++
		report.Findings = append(report.Findings, result.findings...)
	}

	if config.RewriteCommits {
		if !report.Complete {
			report.Errors = append(report.Errors, model.ReportError{Stage: "commit_rewrite", Message: "skipped because the repository scan was incomplete"})
		} else if r.Rewriter == nil {
			addGlobalError(&report, "configuration", fmt.Errorf("commit rewriter is not configured"))
		} else if authenticatedUser.ID <= 0 {
			addGlobalError(&report, "authentication", fmt.Errorf("GitHub user ID is required to generate a noreply replacement email"))
		} else {
			replacementEmail := fmt.Sprintf("%d+%s@users.noreply.github.com", authenticatedUser.ID, authenticatedUser.Login)
			if _, targeted := config.Matcher.Match(replacementEmail); targeted {
				addGlobalError(&report, "commit_rewrite", fmt.Errorf("target email %s is already the replacement noreply address", replacementEmail))
				report.Finalize()
				return report
			}
			repositoriesByName := make(map[string]model.Repository, len(repositories))
			for _, repository := range repositories {
				repositoriesByName[repository.FullName] = repository
			}
			affected := make(map[string]struct{})
			for _, finding := range report.Findings {
				if containsCommitSource(finding.Sources) {
					affected[finding.Repository] = struct{}{}
				}
			}
			affectedNames := make([]string, 0, len(affected))
			for name := range affected {
				affectedNames = append(affectedNames, name)
			}
			sort.Strings(affectedNames)
			for _, name := range affectedNames {
				repository := repositoriesByName[name]
				rewriteResult, rewriteErr := r.Rewriter.Rewrite(ctx, repository, config.Matcher, replacementEmail)
				if rewriteErr != nil {
					report.Complete = false
					report.Errors = append(report.Errors, model.ReportError{Repository: name, Stage: "commit_rewrite", Message: rewriteErr.Error()})
					continue
				}
				if len(rewriteResult.UpdatedRefs) > 0 {
					report.Rewrites = append(report.Rewrites, rewriteResult)
				}
			}
		}
	}
	report.Finalize()
	return report
}

func containsCommitSource(sources []string) bool {
	for _, source := range sources {
		if source == "commit_author" || source == "commit_committer" {
			return true
		}
	}
	return false
}

func repositoryCounts(repositories []model.Repository) (public, private int) {
	for _, repository := range repositories {
		if repository.Private {
			private++
		} else {
			public++
		}
	}
	return public, private
}

func addGlobalError(report *model.Report, stage string, err error) {
	report.Complete = false
	report.Errors = append(report.Errors, model.ReportError{Stage: stage, Message: err.Error()})
}
