package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
	"github.com/italoag/ghcloner/internal/infrastructure/github"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

// ListCommand handles repository listing operations
type ListCommand struct {
	logger shared.Logger
}

// NewListCommand creates a new list command
func NewListCommand(logger shared.Logger) *ListCommand {
	return &ListCommand{
		logger: logger.With(shared.StringField("command", "list")),
	}
}

// Name returns the command name
func (l *ListCommand) Name() string {
	return "list"
}

// Description returns the command description
func (l *ListCommand) Description() string {
	return "List repositories from a GitHub user or organization"
}

// Usage returns the command usage
func (l *ListCommand) Usage() string {
	return `Usage: ghclone list [options] <type> <owner>

ARGUMENTS:
  type    Repository owner type ('user' or 'org')
  owner   GitHub username or organization name

OPTIONS:
  --token <token>         GitHub personal access token
  --skip-forks            Skip forked repositories (default: true)
  --include-forks         Include forked repositories
  --format <format>       Output format ('table', 'json', 'csv') (default: table)
  --sort <field>          Sort by field ('name', 'size', 'updated') (default: name)
  --limit <n>             Limit number of results (default: no limit)
  --min-size <bytes>      Minimum repository size in bytes
  --max-size <bytes>      Maximum repository size in bytes
  --language <lang>       Filter by programming language
  --updated-after <date>  Filter repositories updated after date (YYYY-MM-DD)

EXAMPLES:
  ghclone list user octocat
  ghclone list org microsoft --format json
  ghclone list user torvalds --include-forks --sort size
  ghclone list org kubernetes --language go --limit 20
`
}

// Execute executes the list command
func (l *ListCommand) Execute(ctx context.Context, args []string) error {
	config, err := l.parseListArgs(args)
	if err != nil {
		fmt.Println(l.Usage())
		return err
	}

	return l.executeList(ctx, config)
}

// ListConfig holds list command configuration
type ListConfig struct {
	Type         repository.RepositoryType
	Owner        string
	Token        string
	SkipForks    bool
	Format       string
	Sort         string
	Limit        int
	MinSize      int64
	MaxSize      int64
	Language     string
	UpdatedAfter time.Time
}

// parseListArgs parses list command arguments
func (l *ListCommand) parseListArgs(args []string) (*ListConfig, error) {
	config := &ListConfig{
		SkipForks: true,
		Format:    "table",
		Sort:      "name",
		Limit:     -1,
		MaxSize:   -1,
		Token:     os.Getenv("GITHUB_TOKEN"),
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		switch arg {
		case "--token":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--token requires a value")
			}
			config.Token = args[i+1]
			i += 2
		case "--skip-forks":
			config.SkipForks = true
			i++
		case "--include-forks":
			config.SkipForks = false
			i++
		case "--format":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--format requires a value")
			}
			format := strings.ToLower(args[i+1])
			if format != "table" && format != "json" && format != "csv" {
				return nil, fmt.Errorf("invalid format: %s (must be 'table', 'json', or 'csv')", args[i+1])
			}
			config.Format = format
			i += 2
		case "--sort":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--sort requires a value")
			}
			sort := strings.ToLower(args[i+1])
			if sort != "name" && sort != "size" && sort != "updated" {
				return nil, fmt.Errorf("invalid sort field: %s (must be 'name', 'size', or 'updated')", args[i+1])
			}
			config.Sort = sort
			i += 2
		case "--limit":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--limit requires a value")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil || val <= 0 {
				return nil, fmt.Errorf("invalid limit value: %s", args[i+1])
			}
			config.Limit = val
			i += 2
		case "--min-size":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--min-size requires a value")
			}
			val, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil || val < 0 {
				return nil, fmt.Errorf("invalid min-size value: %s", args[i+1])
			}
			config.MinSize = val
			i += 2
		case "--max-size":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--max-size requires a value")
			}
			val, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil || val < 0 {
				return nil, fmt.Errorf("invalid max-size value: %s", args[i+1])
			}
			config.MaxSize = val
			i += 2
		case "--language":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--language requires a value")
			}
			config.Language = args[i+1]
			i += 2
		case "--updated-after":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--updated-after requires a value")
			}
			date, err := time.Parse("2006-01-02", args[i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid date format: %s (use YYYY-MM-DD)", args[i+1])
			}
			config.UpdatedAfter = date
			i += 2
		default:
			// Positional arguments
			if config.Type == "" {
				switch strings.ToLower(arg) {
				case "user", "users":
					config.Type = repository.RepositoryTypeUser
				case "org", "orgs", "organization":
					config.Type = repository.RepositoryTypeOrganization
				default:
					return nil, fmt.Errorf("invalid type: %s (must be 'user' or 'org')", arg)
				}
			} else if config.Owner == "" {
				config.Owner = arg
			} else {
				return nil, fmt.Errorf("unexpected argument: %s", arg)
			}
			i++
		}
	}

	// Validate required arguments
	if config.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if config.Owner == "" {
		return nil, fmt.Errorf("owner is required")
	}

	return config, nil
}

// executeList executes the list operation
func (l *ListCommand) executeList(ctx context.Context, config *ListConfig) error {
	// Initialize logger
	logger, err := logging.NewConsoleLogger("warn", false) // Quiet for listing
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Initialize GitHub client
	githubClient := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       config.Token,
		UserAgent:   "ghclone/2.0",
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
	filter.MaxSize = config.MaxSize
	filter.UpdatedAfter = config.UpdatedAfter

	if config.Language != "" {
		filter.Languages = []string{config.Language}
	}

	// Fetch repositories
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
	l.sortRepositories(repositories, config.Sort)

	// Apply limit
	if config.Limit > 0 && len(repositories) > config.Limit {
		repositories = repositories[:config.Limit]
	}

	// Display results
	return l.displayRepositories(repositories, config)
}

// sortRepositories sorts repositories by the specified field
func (l *ListCommand) sortRepositories(repos []*repository.Repository, sortBy string) {
	// Simple sorting implementation
	// In a real implementation, you might want to use sort.Slice
	switch sortBy {
	case "name":
		// Already sorted by name from GitHub API
	case "size":
		// Sort by size (largest first)
		for i := 0; i < len(repos)-1; i++ {
			for j := i + 1; j < len(repos); j++ {
				if repos[i].Size < repos[j].Size {
					repos[i], repos[j] = repos[j], repos[i]
				}
			}
		}
	case "updated":
		// Sort by update time (most recent first)
		for i := 0; i < len(repos)-1; i++ {
			for j := i + 1; j < len(repos); j++ {
				if repos[i].UpdatedAt.Before(repos[j].UpdatedAt) {
					repos[i], repos[j] = repos[j], repos[i]
				}
			}
		}
	}
}

// displayRepositories displays repositories in the specified format
func (l *ListCommand) displayRepositories(repos []*repository.Repository, config *ListConfig) error {
	switch config.Format {
	case "table":
		return l.displayTable(repos)
	case "json":
		return l.displayJSON(repos)
	case "csv":
		return l.displayCSV(repos)
	default:
		return fmt.Errorf("unsupported format: %s", config.Format)
	}
}

// displayTable displays repositories in table format
func (l *ListCommand) displayTable(repos []*repository.Repository) error {
	if len(repos) == 0 {
		fmt.Println("No repositories found.")
		return nil
	}

	// Print header
	fmt.Printf("%-30s %-10s %-15s %-12s %-20s\n", "NAME", "SIZE", "LANGUAGE", "FORK", "UPDATED")
	fmt.Println(strings.Repeat("-", 87))

	// Print repositories
	for _, repo := range repos {
		sizeStr := l.formatSize(repo.Size)
		language := repo.Language
		if language == "" {
			language = "N/A"
		}
		fork := "No"
		if repo.IsFork {
			fork = "Yes"
		}
		updated := repo.UpdatedAt.Format("2006-01-02")

		fmt.Printf("%-30s %-10s %-15s %-12s %-20s\n",
			l.truncateString(repo.Name, 30),
			sizeStr,
			l.truncateString(language, 15),
			fork,
			updated)
	}

	fmt.Printf("\nTotal: %d repositories\n", len(repos))
	return nil
}

// displayJSON displays repositories in JSON format
func (l *ListCommand) displayJSON(repos []*repository.Repository) error {
	// Simple JSON output (in production, use json.Marshal)
	fmt.Println("[")
	for i, repo := range repos {
		fmt.Printf("  {\n")
		fmt.Printf("    \"name\": \"%s\",\n", repo.Name)
		fmt.Printf("    \"full_name\": \"%s\",\n", repo.GetFullName())
		fmt.Printf("    \"clone_url\": \"%s\",\n", repo.CloneURL)
		fmt.Printf("    \"size\": %d,\n", repo.Size)
		fmt.Printf("    \"language\": \"%s\",\n", repo.Language)
		fmt.Printf("    \"fork\": %t,\n", repo.IsFork)
		fmt.Printf("    \"default_branch\": \"%s\",\n", repo.DefaultBranch)
		fmt.Printf("    \"updated_at\": \"%s\"\n", repo.UpdatedAt.Format(time.RFC3339))
		if i < len(repos)-1 {
			fmt.Printf("  },\n")
		} else {
			fmt.Printf("  }\n")
		}
	}
	fmt.Println("]")
	return nil
}

// displayCSV displays repositories in CSV format
func (l *ListCommand) displayCSV(repos []*repository.Repository) error {
	// Print CSV header
	fmt.Println("name,full_name,clone_url,size,language,fork,default_branch,updated_at")

	// Print repositories
	for _, repo := range repos {
		fmt.Printf("%s,%s,%s,%d,%s,%t,%s,%s\n",
			repo.Name,
			repo.GetFullName(),
			repo.CloneURL,
			repo.Size,
			repo.Language,
			repo.IsFork,
			repo.DefaultBranch,
			repo.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

// Helper methods

// formatSize formats size in bytes to human readable format
func (l *ListCommand) formatSize(bytes int64) string {
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
func (l *ListCommand) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// HelpCommand provides help information
type HelpCommand struct {
	commands map[string]Command
}

// NewHelpCommand creates a new help command
func NewHelpCommand(commands map[string]Command) *HelpCommand {
	return &HelpCommand{commands: commands}
}

// Name returns the command name
func (h *HelpCommand) Name() string {
	return "help"
}

// Description returns the command description
func (h *HelpCommand) Description() string {
	return "Show help information for commands"
}

// Usage returns the command usage
func (h *HelpCommand) Usage() string {
	return "Usage: ghclone help [command]"
}

// Execute executes the help command
func (h *HelpCommand) Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		// Show general help
		fmt.Println("ghclone v0.2.0 - Concurrent GitHub Repository Cloner")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("  ghclone <command> [arguments]")
		fmt.Println()
		fmt.Println("COMMANDS:")
		for name, cmd := range h.commands {
			fmt.Printf("  %-10s %s\n", name, cmd.Description())
		}
		fmt.Println()
		fmt.Println("Use 'ghclone help <command>' for more information about a command.")
		return nil
	}

	// Show help for specific command
	commandName := args[0]
	command, exists := h.commands[commandName]
	if !exists {
		return fmt.Errorf("unknown command: %s", commandName)
	}

	fmt.Println(command.Usage())
	return nil
}

// VersionCommand shows version information
type VersionCommand struct{}

// NewVersionCommand creates a new version command
func NewVersionCommand() *VersionCommand {
	return &VersionCommand{}
}

// Name returns the command name
func (v *VersionCommand) Name() string {
	return "version"
}

// Description returns the command description
func (v *VersionCommand) Description() string {
	return "Show version information"
}

// Usage returns the command usage
func (v *VersionCommand) Usage() string {
	return "Usage: ghclone version"
}

// Execute executes the version command
func (v *VersionCommand) Execute(ctx context.Context, args []string) error {
	fmt.Println("ghclone v0.2.0")
	fmt.Printf("Go version: %s\n", "go1.24.3")
	fmt.Println("Optimized with:")
	fmt.Println("  - Concurrent processing with ants worker pool")
	fmt.Println("  - Domain-Driven Design architecture")
	fmt.Println("  - SOLID principles compliance")
	fmt.Println("  - Structured logging with zap")
	fmt.Println("  - Comprehensive error handling")
	return nil
}
