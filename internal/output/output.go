package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/james-yusuke/git-email/internal/model"
)

func WriteJSON(writer io.Writer, report model.Report) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func WriteText(writer io.Writer, report model.Report) error {
	complete := "yes"
	if !report.Complete {
		complete = "no"
	}
	if _, err := fmt.Fprintf(writer,
		"Owner: %s\nRepositories: %d discovered, %d scanned\nComplete: %s\n",
		report.Owner, report.RepositoriesDiscovered, report.RepositoriesScanned, complete,
	); err != nil {
		return err
	}

	if len(report.Findings) == 0 {
		if _, err := fmt.Fprintln(writer, "\nNo email addresses found."); err != nil {
			return err
		}
	} else {
		for _, finding := range report.Findings {
			if _, err := fmt.Fprintf(writer,
				"\n%s %s\n  email: %s\n  visibility: %s\n  sources: %s\n  matches: %d\n",
				finding.Status, finding.RepositoryURL, finding.Email, finding.Visibility,
				strings.Join(finding.Sources, ", "), finding.MatchCount,
			); err != nil {
				return err
			}
			if len(finding.Evidence) > 0 {
				if _, err := fmt.Fprintln(writer, "  evidence:"); err != nil {
					return err
				}
				for _, evidence := range finding.Evidence {
					path := ""
					if evidence.Path != "" {
						path = " path=" + evidence.Path
					}
					if _, err := fmt.Fprintf(writer, "    - %s sha=%s%s\n", evidence.Kind, evidence.ObjectSHA, path); err != nil {
						return err
					}
				}
			}
		}
	}

	if len(report.Rewrites) > 0 {
		if _, err := fmt.Fprintln(writer, "\nRewrites:"); err != nil {
			return err
		}
		for _, rewrite := range report.Rewrites {
			if _, err := fmt.Fprintf(writer,
				"  REWRITTEN %s\n    replacement_email: %s\n    updated_refs: %s\n",
				rewrite.RepositoryURL, rewrite.ReplacementEmail, strings.Join(rewrite.UpdatedRefs, ", "),
			); err != nil {
				return err
			}
		}
	}

	if len(report.Errors) > 0 {
		if _, err := fmt.Fprintln(writer, "\nErrors:"); err != nil {
			return err
		}
		for _, reportError := range report.Errors {
			location := reportError.Stage
			if reportError.Repository != "" {
				location = reportError.Repository + "/" + location
			}
			if _, err := fmt.Fprintf(writer, "  - %s: %s\n", location, reportError.Message); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintf(writer,
		"\nSummary: %d repositories with findings, %d email findings, %d matches (%d public, %d private)\n",
		report.Summary.RepositoriesWithFindings,
		report.Summary.EmailFindings,
		report.Summary.Matches,
		report.Summary.PublicFindings,
		report.Summary.PrivateFindings,
	)
	return err
}

func ExitCode(report model.Report) int {
	if !report.Complete || len(report.Errors) > 0 {
		return 2
	}
	if len(report.Findings) > 0 {
		return 1
	}
	return 0
}
