package scan

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const (
	emailPattern = `[A-Za-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+`
	maxEmailLen  = 254
	chunkSize    = 64 << 10
	overlapSize  = 512
)

var emailRE = regexp.MustCompile(emailPattern)

type Matcher struct {
	specified map[string]struct{}
}

func NewMatcher(specified []string) (*Matcher, error) {
	matcher := &Matcher{}
	if len(specified) == 0 {
		return matcher, nil
	}
	matcher.specified = make(map[string]struct{}, len(specified))
	for _, email := range specified {
		normalized := NormalizeEmail(email)
		if !ValidEmail(normalized) {
			return nil, fmt.Errorf("invalid email address %q", email)
		}
		matcher.specified[normalized] = struct{}{}
	}
	return matcher, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidEmail(email string) bool {
	if len(email) == 0 || len(email) > maxEmailLen {
		return false
	}
	match := emailRE.FindStringIndex(email)
	return match != nil && match[0] == 0 && match[1] == len(email)
}

func (m *Matcher) Match(email string) (string, bool) {
	normalized := NormalizeEmail(email)
	if !ValidEmail(normalized) {
		return "", false
	}
	if len(m.specified) > 0 {
		_, ok := m.specified[normalized]
		return normalized, ok
	}
	if isGitHubNoreply(normalized) {
		return "", false
	}
	return normalized, true
}

func (m *Matcher) Targets() []string {
	targets := make([]string, 0, len(m.specified))
	for email := range m.specified {
		targets = append(targets, email)
	}
	sort.Strings(targets)
	return targets
}

func isGitHubNoreply(email string) bool {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	switch domain {
	case "users.noreply.github.com", "noreply.github.com":
		return true
	case "github.com":
		return local == "noreply"
	default:
		return false
	}
}

func (m *Matcher) FindReader(reader io.Reader, found func(string)) error {
	readBuffer := make([]byte, chunkSize)
	carry := make([]byte, 0, overlapSize)
	var totalRead int64
	var lastEmittedStart int64 = -1

	for {
		n, readErr := reader.Read(readBuffer)
		if n == 0 && readErr == nil {
			continue
		}

		baseOffset := totalRead - int64(len(carry))
		data := make([]byte, 0, len(carry)+n)
		data = append(data, carry...)
		data = append(data, readBuffer[:n]...)
		totalRead += int64(n)

		final := readErr == io.EOF
		cutoff := len(data)
		if !final {
			cutoff -= overlapSize
			if cutoff < 0 {
				cutoff = 0
			}
		}

		for _, indices := range emailRE.FindAllIndex(data, -1) {
			if indices[1] > cutoff {
				break
			}
			start := baseOffset + int64(indices[0])
			if start <= lastEmittedStart {
				continue
			}
			lastEmittedStart = start
			if normalized, ok := m.Match(string(data[indices[0]:indices[1]])); ok {
				found(normalized)
			}
		}

		if final {
			return nil
		}
		if readErr != nil {
			return readErr
		}

		carryLength := overlapSize
		if len(data) < carryLength {
			carryLength = len(data)
		}
		carry = append(carry[:0], data[len(data)-carryLength:]...)
	}
}
