package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/james-yusuke/git-email/internal/model"
)

const (
	defaultBaseURL = "https://api.github.com"
	apiVersion     = "2026-03-10"
	userAgent      = "git-email"
	perPage        = 100
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func New(token string) *Client {
	return NewWithBaseURL(token, defaultBaseURL, nil)
}

func NewWithBaseURL(token, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

type apiUser struct {
	Login             string `json:"login"`
	PublicRepos       int    `json:"public_repos"`
	OwnedPrivateRepos int    `json:"owned_private_repos"`
}

type apiRepository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
	Private  bool   `json:"private"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (c *Client) AuthenticatedUser(ctx context.Context) (model.User, error) {
	if c.token == "" {
		return model.User{}, fmt.Errorf("GITHUB_TOKEN is required for a full public/private scan")
	}
	var user apiUser
	if err := c.getJSON(ctx, "/user", nil, &user); err != nil {
		return model.User{}, err
	}
	return model.User{
		Login:             user.Login,
		PublicRepos:       user.PublicRepos,
		OwnedPrivateRepos: user.OwnedPrivateRepos,
	}, nil
}

func (c *Client) OwnedRepositories(ctx context.Context) ([]model.Repository, error) {
	query := url.Values{
		"visibility":  {"all"},
		"affiliation": {"owner"},
	}
	return c.listRepositories(ctx, "/user/repos", query, "")
}

func (c *Client) PublicRepositories(ctx context.Context, owner string) ([]model.Repository, error) {
	query := url.Values{"type": {"owner"}}
	return c.listRepositories(ctx, "/users/"+url.PathEscape(owner)+"/repos", query, owner)
}

func (c *Client) listRepositories(ctx context.Context, path string, query url.Values, expectedOwner string) ([]model.Repository, error) {
	var repositories []model.Repository
	for page := 1; ; page++ {
		pageQuery := cloneValues(query)
		pageQuery.Set("per_page", strconv.Itoa(perPage))
		pageQuery.Set("page", strconv.Itoa(page))

		var response []apiRepository
		if err := c.getJSON(ctx, path, pageQuery, &response); err != nil {
			return nil, err
		}
		for _, repository := range response {
			if expectedOwner != "" && !strings.EqualFold(repository.Owner.Login, expectedOwner) {
				continue
			}
			repositories = append(repositories, model.Repository{
				FullName: repository.FullName,
				HTMLURL:  repository.HTMLURL,
				CloneURL: repository.CloneURL,
				Private:  repository.Private,
			})
		}
		if len(response) < perPage {
			break
		}
	}
	return repositories, nil
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, destination any) error {
	requestURL := c.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("create GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := strings.TrimSpace(string(body))
		var apiError struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &apiError) == nil && apiError.Message != "" {
			message = apiError.Message
		}
		if message == "" {
			message = response.Status
		}
		if response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
			if resetUnix, parseErr := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil {
				message = fmt.Sprintf("GitHub API rate limit exceeded; resets at %s", time.Unix(resetUnix, 0).UTC().Format(time.RFC3339))
			}
		}
		return fmt.Errorf("GitHub API %s returned %s: %s", path, response.Status, message)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode GitHub API response for %s: %w", path, err)
	}
	return nil
}
