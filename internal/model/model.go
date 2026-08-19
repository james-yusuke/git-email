package model

import (
	"sort"
)

const (
	StatusExposed        = "EXPOSED"
	StatusPrivateFinding = "PRIVATE_FINDING"
)

type Repository struct {
	FullName string
	HTMLURL  string
	CloneURL string
	Private  bool
}

func (r Repository) Visibility() string {
	if r.Private {
		return "private"
	}
	return "public"
}

func (r Repository) Status() string {
	if r.Private {
		return StatusPrivateFinding
	}
	return StatusExposed
}

type User struct {
	ID                int64
	Login             string
	PublicRepos       int
	OwnedPrivateRepos int
}

type Evidence struct {
	Kind      string `json:"kind"`
	ObjectSHA string `json:"object_sha"`
	Path      string `json:"path,omitempty"`
}

type Finding struct {
	Repository    string     `json:"repository"`
	RepositoryURL string     `json:"repository_url"`
	Visibility    string     `json:"visibility"`
	Status        string     `json:"status"`
	Email         string     `json:"email"`
	Sources       []string   `json:"sources"`
	MatchCount    int        `json:"match_count"`
	Evidence      []Evidence `json:"evidence"`
}

type ReportError struct {
	Repository string `json:"repository,omitempty"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
}

type RewriteResult struct {
	Repository       string   `json:"repository"`
	RepositoryURL    string   `json:"repository_url"`
	ReplacementEmail string   `json:"replacement_email"`
	UpdatedRefs      []string `json:"updated_refs"`
}

type Summary struct {
	RepositoriesWithFindings int `json:"repositories_with_findings"`
	EmailFindings            int `json:"email_findings"`
	PublicFindings           int `json:"public_findings"`
	PrivateFindings          int `json:"private_findings"`
	Matches                  int `json:"matches"`
}

type Report struct {
	Owner                  string          `json:"owner"`
	Complete               bool            `json:"complete"`
	RepositoriesDiscovered int             `json:"repositories_discovered"`
	RepositoriesScanned    int             `json:"repositories_scanned"`
	Findings               []Finding       `json:"findings"`
	Rewrites               []RewriteResult `json:"rewrites,omitempty"`
	Errors                 []ReportError   `json:"errors,omitempty"`
	Summary                Summary         `json:"summary"`
}

func (r *Report) Finalize() {
	sort.Slice(r.Findings, func(i, j int) bool {
		if r.Findings[i].Repository != r.Findings[j].Repository {
			return r.Findings[i].Repository < r.Findings[j].Repository
		}
		return r.Findings[i].Email < r.Findings[j].Email
	})
	for i := range r.Findings {
		sort.Strings(r.Findings[i].Sources)
		sort.Slice(r.Findings[i].Evidence, func(a, b int) bool {
			left, right := r.Findings[i].Evidence[a], r.Findings[i].Evidence[b]
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			if left.ObjectSHA != right.ObjectSHA {
				return left.ObjectSHA < right.ObjectSHA
			}
			return left.Path < right.Path
		})
	}
	sort.Slice(r.Errors, func(i, j int) bool {
		if r.Errors[i].Repository != r.Errors[j].Repository {
			return r.Errors[i].Repository < r.Errors[j].Repository
		}
		if r.Errors[i].Stage != r.Errors[j].Stage {
			return r.Errors[i].Stage < r.Errors[j].Stage
		}
		return r.Errors[i].Message < r.Errors[j].Message
	})
	sort.Slice(r.Rewrites, func(i, j int) bool {
		return r.Rewrites[i].Repository < r.Rewrites[j].Repository
	})
	for i := range r.Rewrites {
		sort.Strings(r.Rewrites[i].UpdatedRefs)
	}

	var summary Summary
	repositories := make(map[string]struct{})
	for _, finding := range r.Findings {
		repositories[finding.Repository] = struct{}{}
		summary.EmailFindings++
		summary.Matches += finding.MatchCount
		if finding.Visibility == "private" {
			summary.PrivateFindings++
		} else {
			summary.PublicFindings++
		}
	}
	summary.RepositoriesWithFindings = len(repositories)
	r.Summary = summary
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
}
