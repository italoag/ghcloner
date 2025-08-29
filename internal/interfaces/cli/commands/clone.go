package commands

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/italoag/ghcloner/internal/application/services"
	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
	"github.com/italoag/ghcloner/internal/infrastructure/concurrency"
	"github.com/italoag/ghcloner/internal/infrastructure/git"
	"github.com/italoag/ghcloner/internal/infrastructure/github"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

// Command represents a CLI command
type Command interface {
	Name() string
	Description() string
	Usage() string
	Execute(ctx context.Context, args []string) error
}

// CLIApplication manages CLI commands and dependency injection
type CLIApplication struct {
	commands map[string]Command
	logger   shared.Logger
}

// NewCLIApplication creates a new CLI application
func NewCLIApplication() *CLIApplication {
	app := &CLIApplication{
		commands: make(map[string]Command),
	}

	// Initialize logger
	logger, err := logging.NewConsoleLogger("info", true)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	app.logger = logger

	// Register commands
	app.registerCommands()

	return app
}

// registerCommands registers all available commands
func (app *CLIApplication) registerCommands() {
	app.commands["clone"] = NewCloneCommand(app.logger)
	app.commands["list"] = NewListCommand(app.logger)
	app.commands["help"] = NewHelpCommand(app.commands)
	app.commands["version"] = NewVersionCommand()
}

// Execute executes a command with the given arguments
func (app *CLIApplication) Execute(args []string) error {
	if len(args) < 2 {
		return app.showUsage()
	}

	commandName := args[1]
	command, exists := app.commands[commandName]
	if !exists {
		return fmt.Errorf("unknown command: %s", commandName)
	}

	ctx := context.Background()
	return command.Execute(ctx, args[2:])
}

// showUsage shows general usage information
func (app *CLIApplication) showUsage() error {
	fmt.Println("ghclone v2.0 - Concurrent GitHub Repository Cloner")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  ghclone <command> [arguments]")
	fmt.Println()
	fmt.Println("COMMANDS:")
	for name, cmd := range app.commands {
		fmt.Printf("  %-10s %s\n", name, cmd.Description())
	}
	fmt.Println()
	fmt.Println("Use 'ghclone help <command>' for more information about a command.")
	return nil
}

// CloneCommand handles repository cloning operations
type CloneCommand struct {
	logger shared.Logger
}

// NewCloneCommand creates a new clone command
func NewCloneCommand(logger shared.Logger) *CloneCommand {
	return &CloneCommand{
		logger: logger.With(shared.StringField("command", "clone")),
	}
}

// Name returns the command name
func (c *CloneCommand) Name() string {
	return "clone"
}

// Description returns the command description
func (c *CloneCommand) Description() string {
	return "Clone repositories from a GitHub user or organization"
}

// Usage returns the command usage
func (c *CloneCommand) Usage() string {
	return `Usage: ghclone clone [options] <type> <owner>

ARGUMENTS:
  type    Repository owner type ('user' or 'org')
  owner   GitHub username or organization name

OPTIONS:
  --token <token>         GitHub personal access token
  --concurrency <n>       Number of concurrent workers (default: 2x CPU cores)
  --skip-forks            Skip forked repositories (default: true)
  --include-forks         Include forked repositories
  --depth <n>             Clone depth for shallow clones (default: 1)
  --branch <name>         Specific branch to clone (default: default branch)
  --base-dir <path>       Base directory for cloning (default: current directory)
  --log-level <level>     Log level (debug, info, warn, error) (default: info)

EXAMPLES:
  ghclone clone user octocat
  ghclone clone org microsoft --token ghp_abc123
  ghclone clone user torvalds --concurrency 4 --include-forks
  ghclone clone org kubernetes --base-dir /tmp/repos --log-level debug
`
}

// Execute executes the clone command
func (c *CloneCommand) Execute(ctx context.Context, args []string) error {
	config, err := c.parseCloneArgs(args)
	if err != nil {
		fmt.Println(c.Usage())
		return err
	}

	return c.executeClone(ctx, config)
}

// CloneConfig holds clone command configuration
type CloneConfig struct {
	Type        repository.RepositoryType
	Owner       string
	Token       string
	Concurrency int
	SkipForks   bool
	Depth       int
	Branch      string
	BaseDir     string
	LogLevel    string
}

// parseCloneArgs parses clone command arguments
func (c *CloneCommand) parseCloneArgs(args []string) (*CloneConfig, error) {
	config := &CloneConfig{
		Concurrency: runtime.NumCPU() * 2,
		SkipForks:   true,
		Depth:       1,
		BaseDir:     ".",
		LogLevel:    "info",
		Token:       os.Getenv("GITHUB_TOKEN"),
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
		case "--concurrency":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--concurrency requires a value")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil || val <= 0 {
				return nil, fmt.Errorf("invalid concurrency value: %s", args[i+1])
			}
			config.Concurrency = val
			i += 2
		case "--skip-forks":
			config.SkipForks = true
			i++
		case "--include-forks":
			config.SkipForks = false
			i++
		case "--depth":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--depth requires a value")
			}
			val, err := strconv.Atoi(args[i+1])
			if err != nil || val < 0 {
				return nil, fmt.Errorf("invalid depth value: %s", args[i+1])
			}
			config.Depth = val
			i += 2
		case "--branch":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--branch requires a value")
			}
			config.Branch = args[i+1]
			i += 2
		case "--base-dir":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--base-dir requires a value")
			}
			config.BaseDir = args[i+1]
			i += 2
		case "--log-level":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--log-level requires a value")
			}
			config.LogLevel = args[i+1]
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

// executeClone executes the cloning operation
func (c *CloneCommand) executeClone(ctx context.Context, config *CloneConfig) error {
	// Initialize logger with configured level
	logger, err := logging.NewConsoleLogger(config.LogLevel, true)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	logger.Info("Starting clone operation",
		shared.StringField("type", config.Type.String()),
		shared.StringField("owner", config.Owner),
		shared.IntField("concurrency", config.Concurrency),
		shared.StringField("base_dir", config.BaseDir))

	// Initialize GitHub client
	githubClient := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       config.Token,
		UserAgent:   "ghclone/2.0",
		Timeout:     30 * time.Second,
		RateLimiter: github.NewTokenBucketRateLimiter(5000),
		Logger:      logger.With(shared.StringField("component", "github")),
	})

	// Initialize Git client
	gitClient, err := git.NewGitClient(&git.GitClientConfig{
		Timeout: 10 * time.Minute,
		Logger:  logger.With(shared.StringField("component", "git")),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize Git client: %w", err)
	}

	// Validate Git installation
	if err := gitClient.ValidateGitInstallation(ctx); err != nil {
		return fmt.Errorf("Git validation failed: %w", err)
	}

	// Initialize worker pool
	workerPool, err := concurrency.NewWorkerPool(&concurrency.WorkerPoolConfig{
		MaxWorkers: config.Concurrency,
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
		GitClient:  gitClient,
		Logger:     logger.With(shared.StringField("component", "worker_pool")),
	})
	if err != nil {
		return fmt.Errorf("failed to create worker pool: %w", err)
	}
	defer workerPool.Close()

	// Initialize services
	fetchUseCase := usecases.NewFetchRepositoriesUseCase(githubClient, logger)
	cloningService, err := services.NewCloningService(&services.CloningServiceConfig{
		WorkerPool: workerPool,
		GitClient:  gitClient,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create cloning service: %w", err)
	}
	defer cloningService.Close()

	// Fetch repositories
	filter := repository.NewRepositoryFilter()
	filter.IncludeForks = !config.SkipForks

	fetchReq := &usecases.FetchRepositoriesRequest{
		Owner:  config.Owner,
		Type:   config.Type,
		Filter: filter,
	}

	logger.Info("Fetching repositories...")
	fetchResp, err := fetchUseCase.Execute(ctx, fetchReq)
	if err != nil {
		return fmt.Errorf("failed to fetch repositories: %w", err)
	}

	if len(fetchResp.Repositories) == 0 {
		fmt.Printf("No repositories found for %s/%s\n", config.Type, config.Owner)
		return nil
	}

	fmt.Printf("Found %d repositories for %s/%s\n", len(fetchResp.Repositories), config.Type, config.Owner)

	// Prepare clone options
	cloneOptions := &cloning.CloneOptions{
		Depth:             config.Depth,
		RecurseSubmodules: true,
		Branch:            config.Branch,
		SkipExisting:      true,
		CreateOrgDirs:     false,
	}

	// Start cloning
	cloneReq := &services.CloneBatchRequest{
		Repositories:  fetchResp.Repositories,
		BaseDirectory: config.BaseDir,
		Options:       cloneOptions,
		BatchID:       fmt.Sprintf("clone_%d", time.Now().UnixNano()),
	}

	logger.Info("Starting concurrent cloning...")
	cloneResp, err := cloningService.CloneBatch(ctx, cloneReq)
	if err != nil {
		return fmt.Errorf("failed to start cloning: %w", err)
	}

	fmt.Printf("Submitted %d jobs for concurrent cloning with %d workers\n", 
		cloneResp.SubmittedJobs, config.Concurrency)

	// Monitor progress
	return c.monitorProgress(ctx, cloningService, cloneResp.BatchID)
}

// monitorProgress monitors and displays cloning progress
func (c *CloneCommand) monitorProgress(ctx context.Context, service *services.CloningService, batchID string) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastProgress *cloning.Progress

	for {
		select {
		case <-ticker.C:
			progress := service.GetProgress()
			if progress == nil {
				continue
			}

			// Only print if progress changed
			if lastProgress == nil || 
				progress.Completed != lastProgress.Completed ||
				progress.Failed != lastProgress.Failed ||
				progress.Skipped != lastProgress.Skipped {

				fmt.Printf("\rProgress: %d/%d completed, %d failed, %d skipped (%.1f%%) - ETA: %v",
					progress.Completed, progress.Total, progress.Failed, progress.Skipped,
					progress.GetPercentage(), progress.ETA.Truncate(time.Second))

				lastProgress = progress
			}

			if progress.IsComplete() {
				fmt.Printf("\n\nCloning completed!\n")
				fmt.Printf("Results: %d successful, %d failed, %d skipped out of %d total\n",
					progress.Completed, progress.Failed, progress.Skipped, progress.Total)
				fmt.Printf("Total time: %v\n", progress.ElapsedTime.Truncate(time.Second))
				
				if progress.Failed > 0 {
					fmt.Printf("Success rate: %.1f%%\n", progress.GetSuccessRate())
				}
				return nil
			}

		case <-ctx.Done():
			fmt.Printf("\nOperation cancelled\n")
			return ctx.Err()
		}
	}
}