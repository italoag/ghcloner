package fang

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/italoag/repocloner/internal/application/usecases"
	"github.com/italoag/repocloner/internal/domain/cloning"
	"github.com/italoag/repocloner/internal/domain/shared"
	"github.com/italoag/repocloner/internal/infrastructure/bitbucket"
	"github.com/italoag/repocloner/internal/infrastructure/concurrency"
	"github.com/italoag/repocloner/internal/infrastructure/git"
	"github.com/italoag/repocloner/internal/infrastructure/github"
	"github.com/italoag/repocloner/internal/infrastructure/logging"
)

// Application represents the main application with all dependencies
type Application struct {
	logger                   shared.Logger
	githubClient             *github.GitHubClient
	bitbucketClient          *bitbucket.BitbucketClient
	gitClient                *git.GitClient
	workerPool               *concurrency.WorkerPool
	domainService            *cloning.DomainCloneService
	fetchRepositoriesUseCase *usecases.FetchRepositoriesUseCase
	cloneRepositoriesUseCase *usecases.CloneRepositoriesUseCase
}

// NewApplication creates and configures the application with all dependencies
func NewApplication(config *Config) (*Application, *logging.TUILogger, error) {
	// Initialize TUI logger that writes to file and buffers for display
	tuiLogger, err := logging.NewTUILogger(&logging.TUILoggerConfig{
		LogFile:     "logs/repocloner.log",
		Level:       config.LogLevel,
		BufferSize:  50,
		Development: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create TUI logger: %w", err)
	}

	// Use TUILogger as the main logger (satisfies shared.Logger interface)
	logger := shared.Logger(tuiLogger)

	logger.Info("Initializing repocloner application",
		shared.StringField("version", "0.2.0"),
		shared.StringField("go_version", runtime.Version()))

	// Initialize GitHub client
	githubClient := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       config.Token,
		UserAgent:   "repocloner/0.2",
		Timeout:     30 * time.Second,
		RateLimiter: github.NewTokenBucketRateLimiter(5000), // GitHub default limit
		Logger:      logger.With(shared.StringField("component", "github_client")),
	})

	// Validate GitHub token if provided
	if config.Token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := githubClient.ValidateToken(ctx); err != nil {
			logger.Warn("GitHub token validation failed", shared.ErrorField(err))
		} else {
			logger.Info("GitHub token validated successfully")
		}
	}

	// Initialize Bitbucket client
	bitbucketClient := bitbucket.NewBitbucketClient(&bitbucket.BitbucketClientConfig{
		Username:    "x-bitbucket-api-token-auth", // For Git operations
		Email:       config.BitbucketEmail,        // For API operations
		APIToken:    config.BitbucketAPIToken,
		UserAgent:   "repocloner/0.2",
		Timeout:     30 * time.Second,
		RateLimiter: bitbucket.NewTokenBucketRateLimiter(1000), // Bitbucket default limit
		Logger:      logger.With(shared.StringField("component", "bitbucket_client")),
	})

	// Validate Bitbucket credentials if provided
	if config.BitbucketAPIToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := bitbucketClient.ValidateCredentials(ctx); err != nil {
			logger.Warn("Bitbucket credentials validation failed", shared.ErrorField(err))
		} else {
			logger.Info("Bitbucket credentials validated successfully")
		}
	}

	// Initialize Git client
	gitClient, err := git.NewGitClient(&git.GitClientConfig{
		Timeout: 10 * time.Minute,
		Logger:  logger.With(shared.StringField("component", "git_client")),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Git client: %w", err)
	}

	// Validate Git installation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := gitClient.ValidateGitInstallation(ctx); err != nil {
		return nil, nil, fmt.Errorf("git validation failed: %w", err)
	}

	// Initialize worker pool
	maxWorkers := runtime.NumCPU() * 2
	if config.Concurrency > 0 {
		maxWorkers = config.Concurrency
	}

	workerPool, err := concurrency.NewWorkerPool(&concurrency.WorkerPoolConfig{
		MaxWorkers: maxWorkers,
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
		GitClient:  gitClient,
		Logger:     logger.With(shared.StringField("component", "worker_pool")),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create worker pool: %w", err)
	}

	// Initialize domain service
	domainService := cloning.NewDomainCloneService(logger.With(shared.StringField("component", "domain_service")))

	// Initialize use cases
	fetchRepositoriesUseCase := usecases.NewFetchRepositoriesUseCase(
		githubClient,
		bitbucketClient,
		logger.With(shared.StringField("usecase", "fetch_repositories")),
	)

	cloneRepositoriesUseCase := usecases.NewCloneRepositoriesUseCase(
		workerPool,
		domainService,
		logger.With(shared.StringField("usecase", "clone_repositories")),
	)

	logger.Info("Application initialized successfully",
		shared.IntField("max_workers", maxWorkers))

	return &Application{
		logger:                   logger,
		githubClient:             githubClient,
		bitbucketClient:          bitbucketClient,
		gitClient:                gitClient,
		workerPool:               workerPool,
		domainService:            domainService,
		fetchRepositoriesUseCase: fetchRepositoriesUseCase,
		cloneRepositoriesUseCase: cloneRepositoriesUseCase,
	}, tuiLogger, nil
}

// Close gracefully shuts down the application
func (app *Application) Close() error {
	app.logger.Info("Shutting down application")

	if err := app.workerPool.Close(); err != nil {
		app.logger.Error("Failed to close worker pool", shared.ErrorField(err))
	}

	// Close logger if it's a TUILogger
	if tuiLogger, ok := app.logger.(*logging.TUILogger); ok {
		if err := tuiLogger.Close(); err != nil {
			return err
		}
	} else if zapLogger, ok := app.logger.(*logging.ZapLogger); ok {
		if err := zapLogger.Close(); err != nil {
			return err
		}
	}

	return nil
}

// Config holds application configuration
type Config struct {
	Token             string // GitHub token
	BitbucketAPIToken string // Bitbucket API token
	BitbucketEmail    string // Bitbucket Atlassian account email
	Concurrency       int
	LogLevel          string
	BaseDir           string
}

// NewDefaultConfig creates default configuration
func NewDefaultConfig() *Config {
	return &Config{
		Concurrency: runtime.NumCPU() * 2,
		LogLevel:    "info",
		BaseDir:     ".",
	}
}

// RootCommand creates the root cobra command with Fang styling
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repocloner",
		Short: "Concurrent Multi-Provider Repository Cloner",
		Long: `repocloner is a high-performance, concurrent repository cloner built with Go.

It provides enhanced terminal UI experiences with real-time progress tracking,
structured logging, and efficient concurrent processing using worker pools.

Features:
  • Concurrent cloning with configurable worker pools
  • Real-time progress tracking with TUI
  • Structured logging with file output and TUI display
  • Support for GitHub (users and organizations) and Bitbucket (users and workspaces)
  • Advanced filtering and configuration options
  • GitHub API rate limiting and token validation
  • Bitbucket API v2.0 support with API token authentication`,
		Version: "0.2.0",
		Example: `  # Clone all repositories from a GitHub user
  repocloner clone user octocat

  # Clone GitHub organization repositories with custom settings
  repocloner clone org microsoft --concurrency 8 --skip-forks

  # Clone all repositories from a Bitbucket user
  repocloner bitbucket user myusername

  # Clone Bitbucket workspace repositories
  repocloner bitbucket workspace myworkspace --skip-forks

  # List repositories in JSON format
  repocloner list user torvalds --format json --include-forks

  # Generate shell completions
  repocloner completion bash > /etc/bash_completion.d/repocloner`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Add global flags
	cmd.PersistentFlags().String("token", "", "GitHub personal access token (env: GITHUB_TOKEN)")
	cmd.PersistentFlags().String("bitbucket-api-token", "", "Bitbucket API token (env: BITBUCKET_API_TOKEN)")
	cmd.PersistentFlags().String("bitbucket-email", "", "Bitbucket Atlassian account email (env: BITBUCKET_EMAIL)")
	cmd.PersistentFlags().String("log-level", "info", "Log level (debug, info, warn, error)")
	cmd.PersistentFlags().Int("concurrency", runtime.NumCPU()*2, "Number of concurrent workers")
	cmd.PersistentFlags().String("base-dir", ".", "Base directory for operations")

	return cmd
}

// Execute runs the CLI application with Fang enhancements
func Execute(ctx context.Context) error {
	rootCmd := NewRootCommand()

	// Add all subcommands
	rootCmd.AddCommand(NewCloneCommand())
	rootCmd.AddCommand(NewBitbucketCloneCommand())
	rootCmd.AddCommand(NewListCommand())

	// Apply Fang styling and enhancements
	return fang.Execute(ctx, rootCmd)
}

// Helper function to get global config from cobra command
func getGlobalConfig(cmd *cobra.Command) (*Config, error) {
	config := NewDefaultConfig()

	if token, err := cmd.Flags().GetString("token"); err == nil && token != "" {
		config.Token = token
	}

	if token, err := cmd.Flags().GetString("bitbucket-api-token"); err == nil && token != "" {
		config.BitbucketAPIToken = token
	}

	if email, err := cmd.Flags().GetString("bitbucket-email"); err == nil && email != "" {
		config.BitbucketEmail = email
	}

	if logLevel, err := cmd.Flags().GetString("log-level"); err == nil && logLevel != "" {
		config.LogLevel = logLevel
	}

	if concurrency, err := cmd.Flags().GetInt("concurrency"); err == nil && concurrency > 0 {
		config.Concurrency = concurrency
	}

	if baseDir, err := cmd.Flags().GetString("base-dir"); err == nil && baseDir != "" {
		// Convert to absolute path
		if !filepath.IsAbs(baseDir) {
			absPath, err := filepath.Abs(baseDir)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve base directory: %w", err)
			}
			config.BaseDir = absPath
		} else {
			config.BaseDir = baseDir
		}
	}

	return config, nil
}
