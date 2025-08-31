package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
)

// GitHubAPIResponse represents the structure of GitHub API responses
type GitHubAPIResponse struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	CloneURL      string    `json:"clone_url"`
	Fork          bool      `json:"fork"`
	Size          int64     `json:"size"`
	DefaultBranch string    `json:"default_branch"`
	Language      string    `json:"language"`
	Description   string    `json:"description"`
	UpdatedAt     time.Time `json:"updated_at"`
	Owner         OwnerInfo `json:"owner"`
}

// OwnerInfo represents repository owner information
type OwnerInfo struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// RateLimitInfo represents GitHub API rate limit information
type RateLimitInfo struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetTime time.Time `json:"reset_time"`
}

// GitHubClient handles interactions with GitHub API
type GitHubClient struct {
	httpClient  *http.Client
	baseURL     string
	token       string
	userAgent   string
	rateLimiter RateLimiter
	logger      shared.Logger
}

// GitHubClientConfig holds configuration for GitHub client
type GitHubClientConfig struct {
	Token       string
	BaseURL     string
	UserAgent   string
	Timeout     time.Duration
	RateLimiter RateLimiter
	Logger      shared.Logger
}

// NewGitHubClient creates a new GitHub API client
func NewGitHubClient(config *GitHubClientConfig) *GitHubClient {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.github.com"
	}
	if config.UserAgent == "" {
		config.UserAgent = "ghclone/1.0"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &GitHubClient{
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		baseURL:     config.BaseURL,
		token:       config.Token,
		userAgent:   config.UserAgent,
		rateLimiter: config.RateLimiter,
		logger:      config.Logger,
	}
}

// FetchRepositories fetches repositories for a user or organization
func (c *GitHubClient) FetchRepositories(
	ctx context.Context,
	owner string,
	repoType repository.RepositoryType,
	filter *repository.RepositoryFilter,
	pagination *repository.PaginationOptions,
) ([]*repository.Repository, error) {
	if pagination == nil {
		pagination = repository.NewPaginationOptions()
	}

	var repos []*repository.Repository
	page := 1

	for {
		pageRepos, hasMore, err := c.fetchRepositoryPage(ctx, owner, repoType, page, pagination.PerPage)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", page, err)
		}

		// Apply filtering
		for _, repo := range pageRepos {
			if filter == nil || filter.ShouldInclude(repo) {
				repos = append(repos, repo)
			}
		}

		if !hasMore {
			break
		}
		page++

		// Check context cancellation
		if ctx.Err() != nil {
			return repos, ctx.Err()
		}
	}

	c.logger.Info("Successfully fetched repositories",
		shared.StringField("owner", owner),
		shared.StringField("type", repoType.String()),
		shared.IntField("total", len(repos)))

	return repos, nil
}

// fetchRepositoryPage fetches a single page of repositories
func (c *GitHubClient) fetchRepositoryPage(
	ctx context.Context,
	owner string,
	repoType repository.RepositoryType,
	page, perPage int,
) ([]*repository.Repository, bool, error) {
	// Wait for rate limiter
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, false, fmt.Errorf("rate limiter error: %w", err)
		}
	}

	url := fmt.Sprintf("%s/%s/%s/repos?per_page=%d&page=%d",
		c.baseURL, repoType.String(), owner, perPage, page)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Warn("failed to close response body", shared.ErrorField(err))
		}
	}()

	// Update rate limiter with response headers
	if c.rateLimiter != nil {
		c.updateRateLimitFromResponse(resp)
	}

	// Handle different status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Success, continue processing
	case http.StatusNotFound:
		return nil, false, repository.ErrRepositoryNotFound
	case http.StatusUnauthorized:
		return nil, false, fmt.Errorf("authentication failed: check your token")
	case http.StatusForbidden:
		return nil, false, fmt.Errorf("access forbidden: rate limit exceeded or insufficient permissions")
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response body
	var apiRepos []GitHubAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiRepos); err != nil {
		return nil, false, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to domain objects
	repos := make([]*repository.Repository, 0, len(apiRepos))
	for _, apiRepo := range apiRepos {
		repo, err := c.convertToDomainRepository(&apiRepo)
		if err != nil {
			c.logger.Warn("Failed to convert repository",
				shared.StringField("repo", apiRepo.FullName),
				shared.ErrorField(err))
			continue
		}
		repos = append(repos, repo)
	}

	// Check if there are more pages
	hasMore := len(apiRepos) == perPage

	return repos, hasMore, nil
}

// convertToDomainRepository converts GitHub API response to domain repository
func (c *GitHubClient) convertToDomainRepository(apiRepo *GitHubAPIResponse) (*repository.Repository, error) {
	return repository.NewRepository(
		repository.RepositoryID(apiRepo.ID),
		apiRepo.Name,
		apiRepo.CloneURL,
		apiRepo.Owner.Login,
		apiRepo.Fork,
		apiRepo.Size,
		apiRepo.DefaultBranch,
	)
}

// updateRateLimitFromResponse updates rate limiter based on response headers
func (c *GitHubClient) updateRateLimitFromResponse(resp *http.Response) {
	if rateLimiter, ok := c.rateLimiter.(*TokenBucketRateLimiter); ok {
		if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
			if remainingInt, err := strconv.Atoi(remaining); err == nil {
				rateLimiter.UpdateRemaining(remainingInt)
			}
		}

		if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
			if resetInt, err := strconv.ParseInt(reset, 10, 64); err == nil {
				resetTime := time.Unix(resetInt, 0)
				rateLimiter.UpdateResetTime(resetTime)
			}
		}
	}
}

// GetRateLimitInfo returns current rate limit information
func (c *GitHubClient) GetRateLimitInfo(ctx context.Context) (*RateLimitInfo, error) {
	url := fmt.Sprintf("%s/rate_limit", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Warn("failed to close response body", shared.ErrorField(err))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get rate limit info: status %d", resp.StatusCode)
	}

	var rateLimitResponse struct {
		Rate struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
			Reset     int `json:"reset"`
		} `json:"rate"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rateLimitResponse); err != nil {
		return nil, fmt.Errorf("failed to decode rate limit response: %w", err)
	}

	return &RateLimitInfo{
		Limit:     rateLimitResponse.Rate.Limit,
		Remaining: rateLimitResponse.Rate.Remaining,
		ResetTime: time.Unix(int64(rateLimitResponse.Rate.Reset), 0),
	}, nil
}

// ValidateToken checks if the provided token is valid
func (c *GitHubClient) ValidateToken(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("no token provided")
	}

	url := fmt.Sprintf("%s/user", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Warn("failed to close response body", shared.ErrorField(err))
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid token")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
