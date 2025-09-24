package bitbucket

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

// BitbucketAPIResponse represents the structure of Bitbucket API responses
type BitbucketAPIResponse struct {
	UUID        string      `json:"uuid"`
	Name        string      `json:"name"`
	FullName    string      `json:"full_name"`
	Description string      `json:"description"`
	Language    string      `json:"language"`
	Size        int64       `json:"size"`
	UpdatedOn   time.Time   `json:"updated_on"`
	CreatedOn   time.Time   `json:"created_on"`
	IsPrivate   bool        `json:"is_private"`
	Parent      *ParentRepo `json:"parent"`
	Owner       OwnerInfo   `json:"owner"`
	Links       LinksInfo   `json:"links"`
	MainBranch  *MainBranch `json:"mainbranch"`
}

// ParentRepo represents parent repository for forks
type ParentRepo struct {
	UUID     string `json:"uuid"`
	FullName string `json:"full_name"`
}

// OwnerInfo represents repository owner information
type OwnerInfo struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
}

// LinksInfo represents repository links
type LinksInfo struct {
	Clone []CloneLink `json:"clone"`
}

// CloneLink represents clone URLs
type CloneLink struct {
	Name string `json:"name"`
	Href string `json:"href"`
}

// MainBranch represents the main branch information
type MainBranch struct {
	Name string `json:"name"`
}

// BitbucketPageResponse represents paginated API responses
type BitbucketPageResponse struct {
	Values   []BitbucketAPIResponse `json:"values"`
	Page     int                    `json:"page,omitempty"`
	Pagelen  int                    `json:"pagelen"`
	Size     int                    `json:"size"`
	Next     string                 `json:"next,omitempty"`
	Previous string                 `json:"previous,omitempty"`
}

// RateLimitInfo represents Bitbucket API rate limit information
type RateLimitInfo struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetTime time.Time `json:"reset_time"`
}

// BitbucketClient handles interactions with Bitbucket API
type BitbucketClient struct {
	httpClient  *http.Client
	baseURL     string
	username    string
	appPassword string
	userAgent   string
	rateLimiter RateLimiter
	logger      shared.Logger
}

// BitbucketClientConfig holds configuration for Bitbucket client
type BitbucketClientConfig struct {
	Username    string
	AppPassword string
	BaseURL     string
	UserAgent   string
	Timeout     time.Duration
	RateLimiter RateLimiter
	Logger      shared.Logger
}

// NewBitbucketClient creates a new Bitbucket API client
func NewBitbucketClient(config *BitbucketClientConfig) *BitbucketClient {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.bitbucket.org/2.0"
	}
	if config.UserAgent == "" {
		config.UserAgent = "ghclone/1.0"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &BitbucketClient{
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		baseURL:     config.BaseURL,
		username:    config.Username,
		appPassword: config.AppPassword,
		userAgent:   config.UserAgent,
		rateLimiter: config.RateLimiter,
		logger:      config.Logger,
	}
}

// FetchRepositories fetches repositories for a user or workspace
func (c *BitbucketClient) FetchRepositories(
	ctx context.Context,
	owner string,
	repoType repository.RepositoryType,
	filter *repository.RepositoryFilter,
	pagination *repository.PaginationOptions,
) ([]*repository.Repository, error) {
	c.logger.Info("Fetching repositories from Bitbucket",
		shared.StringField("owner", owner),
		shared.StringField("type", repoType.String()),
		shared.IntField("page", pagination.Page),
		shared.IntField("per_page", pagination.PerPage))

	var allRepos []*repository.Repository
	page := pagination.Page
	if page == 0 {
		page = 1
	}

	for {
		repos, hasNext, err := c.fetchRepositoryPage(ctx, owner, repoType, page, pagination.PerPage)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", page, err)
		}

		// Apply filtering
		for _, repo := range repos {
			if filter.ShouldInclude(repo) {
				allRepos = append(allRepos, repo)
			}
		}

		if !hasNext {
			break
		}
		page++
	}

	c.logger.Info("Successfully fetched repositories",
		shared.StringField("owner", owner),
		shared.StringField("type", repoType.String()),
		shared.IntField("total", len(allRepos)))

	return allRepos, nil
}

// fetchRepositoryPage fetches a single page of repositories
func (c *BitbucketClient) fetchRepositoryPage(
	ctx context.Context,
	owner string,
	repoType repository.RepositoryType,
	page, perPage int,
) ([]*repository.Repository, bool, error) {
	// Construct URL based on repository type
	var url string
	switch repoType {
	case repository.RepositoryTypeBitbucketUser:
		url = fmt.Sprintf("%s/repositories/%s", c.baseURL, owner)
	case repository.RepositoryTypeBitbucketWorkspace:
		url = fmt.Sprintf("%s/repositories/%s", c.baseURL, owner)
	default:
		return nil, false, fmt.Errorf("unsupported repository type: %s", repoType)
	}

	// Add pagination parameters
	url += fmt.Sprintf("?page=%d&pagelen=%d", page, perPage)

	// Wait for rate limiter
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, false, fmt.Errorf("rate limiter error: %w", err)
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	// Set authentication
	if c.username != "" && c.appPassword != "" {
		req.SetBasicAuth(c.username, c.appPassword)
	}

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Update rate limiter from response
	c.updateRateLimitFromResponse(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read response body: %w", err)
	}

	var pageResp BitbucketPageResponse
	if err := json.Unmarshal(body, &pageResp); err != nil {
		return nil, false, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to domain repositories
	var repos []*repository.Repository
	for _, apiRepo := range pageResp.Values {
		repo, err := c.convertToDomainRepository(&apiRepo)
		if err != nil {
			c.logger.Warn("Failed to convert repository",
				shared.StringField("repository", apiRepo.FullName),
				shared.ErrorField(err))
			continue
		}
		repos = append(repos, repo)
	}

	hasNext := pageResp.Next != ""
	return repos, hasNext, nil
}

// convertToDomainRepository converts Bitbucket API response to domain repository
func (c *BitbucketClient) convertToDomainRepository(apiRepo *BitbucketAPIResponse) (*repository.Repository, error) {
	// Get clone URL (prefer HTTPS)
	var cloneURL string
	for _, link := range apiRepo.Links.Clone {
		if link.Name == "https" {
			cloneURL = link.Href
			break
		}
	}
	if cloneURL == "" && len(apiRepo.Links.Clone) > 0 {
		cloneURL = apiRepo.Links.Clone[0].Href
	}

	// Get main branch name
	var defaultBranch string
	if apiRepo.MainBranch != nil {
		defaultBranch = apiRepo.MainBranch.Name
	}
	if defaultBranch == "" {
		defaultBranch = "main" // Bitbucket default
	}

	// Check if it's a fork
	isFork := apiRepo.Parent != nil

	// Use UUID as ID (convert to int64 hash for compatibility)
	id := int64(0)
	if len(apiRepo.UUID) > 2 {
		// Simple hash of UUID
		for _, char := range apiRepo.UUID[1 : len(apiRepo.UUID)-1] { // Remove brackets
			id = id*31 + int64(char)
		}
		if id < 0 {
			id = -id
		}
	}

	return repository.NewRepository(
		repository.RepositoryID(id),
		apiRepo.Name,
		cloneURL,
		apiRepo.Owner.Username,
		isFork,
		apiRepo.Size,
		defaultBranch,
	)
}

// updateRateLimitFromResponse updates rate limiter based on response headers
func (c *BitbucketClient) updateRateLimitFromResponse(resp *http.Response) {
	if rateLimiter, ok := c.rateLimiter.(*TokenBucketRateLimiter); ok {
		// Bitbucket uses different header names
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
func (c *BitbucketClient) GetRateLimitInfo(ctx context.Context) (*RateLimitInfo, error) {
	url := fmt.Sprintf("%s/user", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.username != "" && c.appPassword != "" {
		req.SetBasicAuth(c.username, c.appPassword)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	limit, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Limit"))
	remaining, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	reset, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)

	return &RateLimitInfo{
		Limit:     limit,
		Remaining: remaining,
		ResetTime: time.Unix(reset, 0),
	}, nil
}

// ValidateCredentials checks if the provided credentials are valid
func (c *BitbucketClient) ValidateCredentials(ctx context.Context) error {
	url := fmt.Sprintf("%s/user", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	if c.username != "" && c.appPassword != "" {
		req.SetBasicAuth(c.username, c.appPassword)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid credentials")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	return nil
}
