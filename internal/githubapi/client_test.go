package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testClient(handler roundTripFunc) *Client {
	return NewWithBaseURL("test-token", "https://api.test", &http.Client{Transport: handler})
}

func jsonResponse(status int, value any) *http.Response {
	body, _ := json.Marshal(value)
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
}

func TestAuthenticatedUserSendsRequiredHeaders(t *testing.T) {
	client := testClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/user" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := request.Header.Get("X-GitHub-Api-Version"); got != apiVersion {
			t.Fatalf("API version = %q", got)
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"id": 238946603, "login": "james-yusuke", "public_repos": 25, "owned_private_repos": 3,
		}), nil
	})

	user, err := client.AuthenticatedUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 238946603 || user.Login != "james-yusuke" || user.PublicRepos != 25 || user.OwnedPrivateRepos != 3 || !user.OwnedPrivateReposReported {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestAuthenticatedUserAllowsMissingPrivateRepositoryCount(t *testing.T) {
	client := testClient(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"id": 238946603, "login": "james-yusuke", "public_repos": 22,
		}), nil
	})

	user, err := client.AuthenticatedUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.OwnedPrivateReposReported || user.OwnedPrivateRepos != 0 {
		t.Fatalf("unexpected private repository count: %+v", user)
	}
}

func TestOwnedRepositoriesPaginates(t *testing.T) {
	client := testClient(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/user/repos" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("visibility") != "all" || request.URL.Query().Get("affiliation") != "owner" {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		count := perPage
		if page == 2 {
			count = 1
		}
		response := make([]map[string]any, 0, count)
		for index := 0; index < count; index++ {
			name := fmt.Sprintf("owner/repo-%03d", (page-1)*perPage+index)
			response = append(response, map[string]any{
				"full_name": name,
				"html_url":  "https://github.com/" + name,
				"clone_url": "https://github.com/" + name + ".git",
				"private":   page == 2,
				"owner":     map[string]string{"login": "owner"},
			})
		}
		return jsonResponse(http.StatusOK, response), nil
	})

	repositories, err := client.OwnedRepositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 101 {
		t.Fatalf("repository count = %d, want 101", len(repositories))
	}
	if !repositories[100].Private {
		t.Fatal("expected final repository to be private")
	}
}

func TestPublicRepositoriesFiltersUnexpectedOwners(t *testing.T) {
	client := NewWithBaseURL("", "https://api.test", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []map[string]any{
			{"full_name": "owner/kept", "owner": map[string]string{"login": "OWNER"}},
			{"full_name": "someone/ignored", "owner": map[string]string{"login": "someone"}},
		}), nil
	})})
	repositories, err := client.PublicRepositories(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].FullName != "owner/kept" {
		t.Fatalf("repositories = %+v", repositories)
	}
}

func TestRateLimitErrorIncludesReset(t *testing.T) {
	client := NewWithBaseURL("", "https://api.test", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := jsonResponse(http.StatusForbidden, map[string]string{"message": "rate limited"})
		response.Header.Set("X-RateLimit-Remaining", "0")
		response.Header.Set("X-RateLimit-Reset", "2000000000")
		return response, nil
	})})
	_, err := client.PublicRepositories(context.Background(), "owner")
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") || !strings.Contains(err.Error(), "2033") {
		t.Fatalf("unexpected error: %v", err)
	}
}
