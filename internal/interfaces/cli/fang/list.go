package fang

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/infrastructure/github"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

// ListConfig holds list command configuration
type ListConfig struct {
	Type         repository.RepositoryType
	Owner        string
	SkipForks    bool
	Format       string
	Sort         string
	Limit        int
	MinSize      int64
	MaxSize      int64
	Language     string
	UpdatedAfter time.Time
}

// NewListCommand creates the list subcommand
func NewListCommand() *cobra.Command {
	var listConfig ListConfig

	cmd := &cobra.Command{
		Use:   "list [type] [owner]",
		Short: "List repositories from a GitHub user or organization",
		Long: `List repositories from a GitHub user or organization with advanced filtering options.

The list command fetches repository information from GitHub and displays it in various
formats including table, JSON, and CSV. It supports comprehensive filtering by size,
language, fork status, and update date.

Repository Types:
  user, users         List repositories from a GitHub user account
  org, orgs           List repositories from a GitHub organization

Output Formats:
  table              Human-readable table format (default)
  json               JSON format for programmatic processing
  csv                CSV format for spreadsheet import

Sorting Options:
  name               Sort by repository name (default)
  size               Sort by repository size (largest first)
  updated            Sort by last update time (most recent first)`,
		Example: `  # List user repositories in table format
  ghclone list user octocat

  # List organization repositories in JSON format
  ghclone list org microsoft --format json

  # List repositories with filtering
  ghclone list user torvalds --include-forks --language c --limit 20

  # List repositories by size with custom filters
  ghclone list org kubernetes --sort size --min-size 1000000 --format csv`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListCommand(cmd, args, &listConfig)
		},
	}

	// Command-specific flags
	cmd.Flags().BoolVar(&listConfig.SkipForks, "skip-forks", true, "Skip forked repositories")
	cmd.Flags().Bool("include-forks", false, "Include forked repositories (inverse of --skip-forks)")
	cmd.Flags().StringVar(&listConfig.Format, "format", "table", "Output format (table, json, csv)")
	cmd.Flags().StringVar(&listConfig.Sort, "sort", "name", "Sort by field (name, size, updated)")
	cmd.Flags().IntVar(&listConfig.Limit, "limit", -1, "Limit number of results")
	cmd.Flags().Int64Var(&listConfig.MinSize, "min-size", 0, "Minimum repository size in bytes")
	cmd.Flags().Int64Var(&listConfig.MaxSize, "max-size", -1, "Maximum repository size in bytes")
	cmd.Flags().StringVar(&listConfig.Language, "language", "", "Filter by programming language")
	cmd.Flags().String("updated-after", "", "Filter repositories updated after date (YYYY-MM-DD)")

	return cmd
}

// runListCommand executes the list command logic
func runListCommand(cmd *cobra.Command, args []string, listConfig *ListConfig) error {
	// Parse and validate arguments
	typeStr := strings.ToLower(args[0])
	owner := args[1]

	switch typeStr {
	case "user", "users":
		listConfig.Type = repository.RepositoryTypeUser
	case "org", "orgs", "organization":
		listConfig.Type = repository.RepositoryTypeOrganization
	default:
		return fmt.Errorf("invalid repository type '%s', must be 'user' or 'org'", typeStr)
	}

	listConfig.Owner = owner

	// Handle include-forks flag (inverse of skip-forks)
	if includeForks, _ := cmd.Flags().GetBool("include-forks"); includeForks {
		listConfig.SkipForks = false
	}

	// Parse updated-after date
	if updatedAfterStr, _ := cmd.Flags().GetString("updated-after"); updatedAfterStr != "" {
		updatedAfter, err := time.Parse("2006-01-02", updatedAfterStr)
		if err != nil {
			return fmt.Errorf("invalid date format for --updated-after: %s (use YYYY-MM-DD)", updatedAfterStr)
		}
		listConfig.UpdatedAfter = updatedAfter
	}

	// Validate format
	switch listConfig.Format {
	case "table", "json", "csv":
		// Valid formats
	default:
		return fmt.Errorf("invalid format '%s', must be 'table', 'json', or 'csv'", listConfig.Format)
	}

	// Validate sort field
	switch listConfig.Sort {
	case "name", "size", "updated":
		// Valid sort fields
	default:
		return fmt.Errorf("invalid sort field '%s', must be 'name', 'size', or 'updated'", listConfig.Sort)
	}

	// Get global configuration
	globalConfig, err := getGlobalConfig(cmd)
	if err != nil {
		return fmt.Errorf("failed to get global configuration: %w", err)
	}

	// Override token from environment if not set
	if globalConfig.Token == "" {
		globalConfig.Token = os.Getenv("GITHUB_TOKEN")
	}

	// Execute list operation
	return executeList(listConfig, globalConfig)
}

// executeList executes the list operation
func executeList(config *ListConfig, globalConfig *Config) error {
	// Initialize logger (quiet for listing)
	logger, err := logging.NewConsoleLogger("warn", false)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Initialize GitHub client
	githubClient := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       globalConfig.Token,
		UserAgent:   "ghclone/0.2",
		Timeout:     30 * time.Second,
		RateLimiter: github.NewTokenBucketRateLimiter(5000),
		Logger:      logger,
	})

	// Initialize use case
	fetchUseCase := usecases.NewFetchRepositoriesUseCase(githubClient, logger)

	// Prepare filter
	filter := repository.NewRepositoryFilter()
	filter.IncludeForks = !config.SkipForks
	filter.MinSize = config.MinSize
	if config.MaxSize > 0 {
		filter.MaxSize = config.MaxSize
	}
	if !config.UpdatedAfter.IsZero() {
		filter.UpdatedAfter = config.UpdatedAfter
	}

	if config.Language != "" {
		filter.Languages = []string{config.Language}
	}

	// Fetch repositories
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fetchReq := &usecases.FetchRepositoriesRequest{
		Owner:  config.Owner,
		Type:   config.Type,
		Filter: filter,
	}

	fetchResp, err := fetchUseCase.Execute(ctx, fetchReq)
	if err != nil {
		return fmt.Errorf("failed to fetch repositories: %w", err)
	}

	repositories := fetchResp.Repositories

	// Sort repositories
	sortRepositories(repositories, config.Sort)

	// Apply limit
	if config.Limit > 0 && len(repositories) > config.Limit {
		repositories = repositories[:config.Limit]
	}

	// Display results
	return displayRepositories(repositories, config)
}

// sortRepositories sorts repositories by the specified field
func sortRepositories(repos []*repository.Repository, sortBy string) {
	switch sortBy {
	case "name":
		sort.Slice(repos, func(i, j int) bool {
			return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
		})
	case "size":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].Size > repos[j].Size // Largest first
		})
	case "updated":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].UpdatedAt.After(repos[j].UpdatedAt) // Most recent first
		})
	}
}

// displayRepositories displays repositories in the specified format
func displayRepositories(repos []*repository.Repository, config *ListConfig) error {
	switch config.Format {
	case "table":
		return displayTable(repos)
	case "json":
		return displayJSON(repos)
	case "csv":
		return displayCSV(repos)
	default:
		return fmt.Errorf("unsupported format: %s", config.Format)
	}
}

// displayTable displays repositories in table format
func displayTable(repos []*repository.Repository) error {
	if len(repos) == 0 {
		fmt.Println("No repositories found.")
		return nil
	}

	// Print header
	fmt.Printf("%-30s %-10s %-15s %-8s %-20s\n", "NAME", "SIZE", "LANGUAGE", "FORK", "UPDATED")
	fmt.Println(strings.Repeat("-", 83))

	// Print repositories
	for _, repo := range repos {
		sizeStr := formatSize(repo.Size)
		language := repo.Language
		if language == "" {
			language = "N/A"
		}
		fork := "No"
		if repo.IsFork {
			fork = "Yes"
		}
		updated := repo.UpdatedAt.Format("2006-01-02")

		fmt.Printf("%-30s %-10s %-15s %-8s %-20s\n",
			truncateString(repo.Name, 30),
			sizeStr,
			truncateString(language, 15),
			fork,
			updated)
	}

	fmt.Printf("\nTotal: %d repositories\n", len(repos))
	return nil
}

// displayJSON displays repositories in JSON format
func displayJSON(repos []*repository.Repository) error {
	// Create a simplified structure for JSON output
	type jsonRepo struct {
		Name          string    `json:"name"`
		FullName      string    `json:"full_name"`
		CloneURL      string    `json:"clone_url"`
		Size          int64     `json:"size"`
		Language      string    `json:"language"`
		Fork          bool      `json:"fork"`
		DefaultBranch string    `json:"default_branch"`
		UpdatedAt     time.Time `json:"updated_at"`
		Description   string    `json:"description,omitempty"`
	}

	jsonRepos := make([]jsonRepo, len(repos))
	for i, repo := range repos {
		jsonRepos[i] = jsonRepo{
			Name:          repo.Name,
			FullName:      repo.GetFullName(),
			CloneURL:      repo.CloneURL,
			Size:          repo.Size,
			Language:      repo.Language,
			Fork:          repo.IsFork,
			DefaultBranch: repo.DefaultBranch,
			UpdatedAt:     repo.UpdatedAt,
			Description:   repo.Description,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonRepos)
}

// displayCSV displays repositories in CSV format
func displayCSV(repos []*repository.Repository) error {
	// Print CSV header
	fmt.Println("name,full_name,clone_url,size,language,fork,default_branch,updated_at,description")

	// Print repositories
	for _, repo := range repos {
		// Escape quotes in description
		description := strings.ReplaceAll(repo.Description, `"`, `""`)
		
		fmt.Printf(`"%s","%s","%s",%d,"%s",%t,"%s","%s","%s"`+"\n",
			repo.Name,
			repo.GetFullName(),
			repo.CloneURL,
			repo.Size,
			repo.Language,
			repo.IsFork,
			repo.DefaultBranch,
			repo.UpdatedAt.Format(time.RFC3339),
			description)
	}
	return nil
}

// Helper functions

// formatSize formats size in bytes to human readable format
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	} else {
		return fmt.Sprintf("%.1fGB", float64(bytes)/(1024*1024*1024))
	}
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}