package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/james-yusuke/git-email/internal/audit"
	"github.com/james-yusuke/git-email/internal/githubapi"
	"github.com/james-yusuke/git-email/internal/output"
	"github.com/james-yusuke/git-email/internal/scan"
)

var ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

type emailFlags []string

func (e *emailFlags) String() string {
	return strings.Join(*e, ",")
}

func (e *emailFlags) Set(value string) error {
	*e = append(*e, value)
	return nil
}

func Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] != "scan" {
		fmt.Fprintln(stderr, "error: expected the scan command")
		writeUsage(stderr)
		return 2
	}

	flags := flag.NewFlagSet("git-email scan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var emails emailFlags
	flags.Var(&emails, "email", "email address to match exactly; may be repeated")
	format := flags.String("format", "text", "output format: text or json")
	jobs := flags.Int("jobs", 4, "number of repositories to scan concurrently")
	publicOnly := flags.Bool("public-only", false, "scan public repositories without requiring private access")
	if err := flags.Parse(arguments[1:]); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		writeUsage(stderr)
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "error: provide exactly one GitHub owner or profile URL")
		writeUsage(stderr)
		return 2
	}
	owner, err := ParseOwner(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "error: unsupported output format %q (use text or json)\n", *format)
		return 2
	}
	if *jobs < 1 {
		fmt.Fprintln(stderr, "error: --jobs must be at least 1")
		return 2
	}
	matcher, err := scan.NewMatcher(emails)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if !*publicOnly && token == "" {
		fmt.Fprintln(stderr, "error: GITHUB_TOKEN is required unless --public-only is used")
		return 2
	}
	executable, err := os.Executable()
	if err != nil && token != "" {
		fmt.Fprintf(stderr, "error: locate executable for secure Git authentication: %v\n", err)
		return 2
	}

	client := githubapi.New(token)
	gitScanner := &scan.GitScanner{Token: token, AskPassPath: executable}
	runner := &audit.Runner{Source: client, Scanner: gitScanner, Jobs: *jobs}
	report := runner.Run(ctx, audit.Config{Owner: owner, PublicOnly: *publicOnly, Matcher: matcher})
	if *format == "json" {
		err = output.WriteJSON(stdout, report)
	} else {
		err = output.WriteText(stdout, report)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: write report: %v\n", err)
		return 2
	}
	return output.ExitCode(report)
}

func ParseOwner(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("GitHub owner cannot be empty")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub profile URL: %w", err)
		}
		if parsed.Scheme != "https" || (parsed.Hostname() != "github.com" && parsed.Hostname() != "www.github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("profile URL must be an https://github.com/<owner> URL")
		}
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) != 1 {
			return "", fmt.Errorf("profile URL must contain exactly one owner")
		}
		decoded, err := url.PathUnescape(parts[0])
		if err != nil {
			return "", fmt.Errorf("invalid owner in profile URL: %w", err)
		}
		value = decoded
	}
	if !ownerPattern.MatchString(value) || strings.Contains(value, "--") {
		return "", fmt.Errorf("invalid GitHub owner %q", value)
	}
	return value, nil
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: git-email scan [--email ADDRESS] [--format text|json] [--jobs N] [--public-only] <owner|profile-url>")
}
