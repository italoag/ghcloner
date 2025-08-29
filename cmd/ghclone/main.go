package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
	"github.com/italoag/ghcloner/internal/infrastructure/concurrency"
	"github.com/italoag/ghcloner/internal/infrastructure/git"
	"github.com/italoag/ghcloner/internal/infrastructure/github"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

// Application represents the main application with all dependencies
type Application struct {
	logger                     shared.Logger
	githubClient              *github.GitHubClient
	gitClient                 *git.GitClient
	workerPool                *concurrency.WorkerPool
	domainService             *cloning.DomainCloneService
	fetchRepositoriesUseCase  *usecases.FetchRepositoriesUseCase
	cloneRepositoriesUseCase  *usecases.CloneRepositoriesUseCase
}

// NewApplication creates and configures the application with all dependencies
func NewApplication(config *Config) (*Application, *logging.TUILogger, error) {
	// Initialize TUI logger that writes to file and buffers for display
	tuiLogger, err := logging.NewTUILogger(&logging.TUILoggerConfig{
		LogFile:     "logs/ghclone.log",
		Level:       "info",
		BufferSize:  50,
		Development: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create TUI logger: %w", err)
	}

	// Use TUILogger as the main logger (satisfies shared.Logger interface)
	logger := shared.Logger(tuiLogger)

	logger.Info("Initializing ghclone application",
		shared.StringField("version", "2.0.0"),
		shared.StringField("go_version", runtime.Version()))

	// Initialize GitHub client
	githubClient := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       config.Token,
		UserAgent:   "ghclone/2.0",
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
		return nil, nil, fmt.Errorf("Git validation failed: %w", err)
	}

	// Initialize worker pool
	maxWorkers := runtime.NumCPU() * 2
	if config.Concurrency > 0 {
		maxWorkers = config.Concurrency
	}

	workerPool, err := concurrency.NewWorkerPool(&concurrency.WorkerPoolConfig{
		MaxWorkers:  maxWorkers,
		MaxRetries:  3,
		RetryDelay:  5 * time.Second,
		GitClient:   gitClient,
		Logger:      logger.With(shared.StringField("component", "worker_pool")),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create worker pool: %w", err)
	}

	// Initialize domain service
	domainService := cloning.NewDomainCloneService()

	// Initialize use cases
	fetchRepositoriesUseCase := usecases.NewFetchRepositoriesUseCase(
		githubClient,
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
		logger:                     logger,
		githubClient:              githubClient,
		gitClient:                 gitClient,
		workerPool:                workerPool,
		domainService:             domainService,
		fetchRepositoriesUseCase:  fetchRepositoriesUseCase,
		cloneRepositoriesUseCase:  cloneRepositoriesUseCase,
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
	Owner       string
	RepoType    repository.RepositoryType
	Token       string
	Concurrency int
	SkipForks   bool
}

// NewConfigFromArgs creates configuration from command line arguments
func NewConfigFromArgs(args []string) (*Config, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("usage: %s <type> <owner> [token]", args[0])
	}

	repoTypeStr := args[1]
	owner := args[2]
	token := os.Getenv("GITHUB_TOKEN")

	if len(args) >= 4 && args[3] != "" {
		token = args[3]
	}

	var repoType repository.RepositoryType
	switch repoTypeStr {
	case "users", "user":
		repoType = repository.RepositoryTypeUser
	case "orgs", "org", "organization":
		repoType = repository.RepositoryTypeOrganization
	default:
		return nil, fmt.Errorf("invalid repository type '%s', must be 'users' or 'orgs'", repoTypeStr)
	}

	return &Config{
		Owner:       owner,
		RepoType:    repoType,
		Token:       token,
		Concurrency: runtime.NumCPU() * 2,
		SkipForks:   true,
	}, nil
}

// TUI Model for Bubble Tea
type tuiModel struct {
	app              *Application
	config           *Config
	repos            []*repository.Repository
	current          int
	total            int
	progress         progress.Model
	quitting         bool
	err              error
	progressTracker  *cloning.ProgressTracker
	useCase          *usecases.CloneRepositoriesUseCase
	tuiLogger        *logging.TUILogger
	logHeight        int
	showLogs         bool
	actualProgress   *cloning.Progress // Store actual progress for display
}

func newTUIModel(app *Application, config *Config, tuiLogger *logging.TUILogger) tuiModel {
	return tuiModel{
		app:       app,
		config:    config,
		progress:  progress.New(progress.WithDefaultGradient()),
		tuiLogger: tuiLogger,
		logHeight: 8, // Show last 8 log entries
		showLogs:  true,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return fetchRepositoriesCmd(m.app, m.config)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "l":
			// Toggle log visibility
			m.showLogs = !m.showLogs
			return m, nil
		case "c":
			// Clear log buffer
			if m.tuiLogger != nil {
				m.tuiLogger.GetLogBuffer().Clear()
			}
			return m, nil
		}
		return m, nil

	case repositoriesMsg:
		m.repos = msg.repositories
		m.total = len(msg.repositories)
		if m.total == 0 {
			m.err = fmt.Errorf("no repositories found for %s/%s", m.config.RepoType, m.config.Owner)
			m.quitting = true
			return m, tea.Quit
		}
		
		// Start concurrent cloning
		return m, startCloningCmd(m.app, m.repos, m.config.Owner)

	case cloningStartedMsg:
		// Start real-time progress tracking
		return m, realProgressTickCmd(m.app)

	case cloningProgressMsg:
		// Always continue progress tracking, even if progress is nil
		if msg.progress != nil {
			m.actualProgress = msg.progress
			percent := float64(msg.progress.Completed+msg.progress.Failed+msg.progress.Skipped) / float64(msg.progress.Total)
			cmd := m.progress.SetPercent(percent)
			
			// Check for completion with stricter condition - ensure we reach exactly 100%
			if msg.progress.IsComplete() && percent >= 1.0 {
				// Force progress bar to 100% and then quit
				finalCmd := m.progress.SetPercent(1.0)
				m.quitting = true
				return m, tea.Batch(finalCmd, tea.Sequence(
					tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
						return tea.Quit()
					}),
				))
			}
			
			// Check for edge case where all jobs are processed but some still show as InProgress
			processed := msg.progress.Completed + msg.progress.Failed + msg.progress.Skipped
			if processed >= msg.progress.Total && msg.progress.InProgress > 0 {
				// This is the bug case - force completion
				finalCmd := m.progress.SetPercent(1.0)
				m.quitting = true
				return m, tea.Batch(finalCmd, tea.Sequence(
					tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
						return tea.Quit()
					}),
				))
			}
			
			return m, tea.Batch(cmd, realProgressTickCmd(m.app), logUpdateCmd())
		} else {
			// Progress is nil, but keep tracking
			return m, tea.Batch(realProgressTickCmd(m.app), logUpdateCmd())
		}

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case logUpdateMsg:
		// Log buffer updated, trigger re-render
		return m, logUpdateCmd()

	case errorMsg:
		m.err = msg.err
		m.quitting = true
		return m, tea.Quit

	default:
		return m, nil
	}

	return m, nil
}

func (m tuiModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("\nError: %v\n\nPress 'q' to exit\n", m.err)
	}
	
	if m.quitting {
		if m.total == 0 {
			return "\nNo repositories found.\n"
		}
		
		// Show completion summary with final statistics
		var completionMsg strings.Builder
		completionMsg.WriteString(fmt.Sprintf("\n✅ Cloning completed: %d repositories processed\n", m.total))
		completionMsg.WriteString(fmt.Sprintf("📁 Directory: %s\n", m.config.Owner))
		
		if m.actualProgress != nil {
			completionMsg.WriteString(fmt.Sprintf("📊 Results: ✅ %d completed, ❌ %d failed, ⏭️ %d skipped\n", 
				m.actualProgress.Completed, m.actualProgress.Failed, m.actualProgress.Skipped))
			if m.actualProgress.ElapsedTime > 0 {
				completionMsg.WriteString(fmt.Sprintf("⏱️ Duration: %v\n", m.actualProgress.ElapsedTime.Truncate(time.Second)))
			}
		}
		
		if m.tuiLogger != nil {
			completionMsg.WriteString(fmt.Sprintf("📄 Log file: %s\n", m.tuiLogger.GetLogFile()))
		}
		
		return completionMsg.String()
	}

	if len(m.repos) == 0 {
		return "\nFetching repositories...\n"
	}

	// Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Render("🚀 ghclone v2.0 - Concurrent GitHub Repository Cloner")

	// Progress info
	info := fmt.Sprintf("Cloning repositories to '%s' directory...", m.config.Owner)
	progressInfo := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7D56F4")).
		Bold(true).
		Render(info)

	// Progress bar
	bar := m.progress.View()

	// Progress details if we have actual progress
	var progressDetails string
	if m.actualProgress != nil {
		progressDetails = m.renderProgressDetails()
		
		// Show completion status when 100% is reached
		if m.actualProgress.IsComplete() {
			percentage := m.actualProgress.GetPercentage()
			if percentage >= 100.0 {
				successStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#04B575")).
					Bold(true)
				progressDetails += "\n" + successStyle.Render("🎉 All repositories processed! Preparing to exit...")
			}
		}
	}

	// Recent completion info
	var recentCompletion string
	if m.actualProgress != nil && m.actualProgress.RecentCompletion != nil {
		recentCompletion = m.renderRecentCompletion()
	}

	// Build the main content
	content := []string{
		header,
		"",
		progressInfo,
		bar,
	}

	// Add progress details if available
	if progressDetails != "" {
		content = append(content, progressDetails)
	}

	// Add recent completion if available
	if recentCompletion != "" {
		content = append(content, "", recentCompletion)
	}

	// Add log section if enabled
	if m.showLogs && m.tuiLogger != nil {
		content = append(content, "", m.renderLogs())
	}

	// Add help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginTop(1)
	
	helpText := "Press 'q' to quit"
	if m.tuiLogger != nil {
		if m.showLogs {
			helpText += " • 'l' to hide logs • 'c' to clear logs"
		} else {
			helpText += " • 'l' to show logs"
		}
	}
	
	content = append(content, helpStyle.Render(helpText))

	return lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, content...),
	)
}

// renderProgressDetails renders detailed progress information
func (m tuiModel) renderProgressDetails() string {
	if m.actualProgress == nil {
		return ""
	}

	p := m.actualProgress
	details := fmt.Sprintf(
		"Progress: %d/%d repositories | ✓ %d completed | ✗ %d failed | ⏭ %d skipped | ⏳ %d in progress",
		p.Completed+p.Failed+p.Skipped+p.InProgress, p.Total,
		p.Completed, p.Failed, p.Skipped, p.InProgress,
	)

	if p.Throughput > 0 {
		details += fmt.Sprintf(" | %.1f repos/sec", p.Throughput)
	}

	if p.ETA > 0 {
		details += fmt.Sprintf(" | ETA: %s", p.ETA.Truncate(time.Second))
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#909090")).
		Render(details)
}

// renderRecentCompletion renders information about the most recently completed repository
func (m tuiModel) renderRecentCompletion() string {
	if m.actualProgress == nil || m.actualProgress.RecentCompletion == nil {
		return ""
	}

	recent := m.actualProgress.RecentCompletion
	var statusIcon, statusColor string

	switch recent.Status {
	case cloning.JobStatusCompleted:
		statusIcon = "✓"
		statusColor = "#04B575" // Green
	case cloning.JobStatusFailed:
		statusIcon = "✗"
		statusColor = "#FF5F87" // Red
	case cloning.JobStatusSkipped:
		statusIcon = "⏭"
		statusColor = "#FFAF00" // Yellow
	default:
		statusIcon = "?"
		statusColor = "#909090" // Gray
	}

	// Format the repository name and status
	repoInfo := fmt.Sprintf("%s %s", statusIcon, recent.Repository)
	if recent.Duration > 0 {
		repoInfo += fmt.Sprintf(" (%s)", recent.Duration.Truncate(time.Millisecond*10))
	}

	if recent.Size > 0 {
		repoInfo += fmt.Sprintf(" [%s]", formatBytes(recent.Size))
	}

	if recent.Error != "" && recent.Status != cloning.JobStatusCompleted {
		// Truncate error message if too long
		errorMsg := recent.Error
		if len(errorMsg) > 60 {
			errorMsg = errorMsg[:57] + "..."
		}
		repoInfo += fmt.Sprintf(" - %s", errorMsg)
	}

	title := "Recently completed:"
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7D56F4")).
		Bold(true)

	repoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(statusColor))

	return titleStyle.Render(title) + " " + repoStyle.Render(repoInfo)
}

// formatBytes formats byte size in human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// renderLogs renders the log display area
func (m tuiModel) renderLogs() string {
	if m.tuiLogger == nil {
		return ""
	}

	// Get recent log entries
	entries := m.tuiLogger.GetLogBuffer().GetRecent(m.logHeight)
	
	if len(entries) == 0 {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(0, 1).
			Width(80).
			Height(m.logHeight).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No logs available")
	}

	// Format log entries
	var logLines []string
	for _, entry := range entries {
		var style lipgloss.Style
		switch entry.Level {
		case "ERROR", "FATAL":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
		case "WARN":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00"))
		case "INFO":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
		case "DEBUG":
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
		default:
			style = lipgloss.NewStyle()
		}
		
		logLine := fmt.Sprintf("[%s] %s %s", 
			entry.Level,
			entry.Timestamp.Format("15:04:05"),
			entry.Message)
		
		logLines = append(logLines, style.Render(logLine))
	}

	// Pad with empty lines if needed
	for len(logLines) < m.logHeight {
		logLines = append(logLines, "")
	}

	// Create bordered log area
	logContent := lipgloss.JoinVertical(lipgloss.Left, logLines...)
	
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#874BFD")).
		Padding(0, 1).
		Width(80).
		Height(m.logHeight + 2). // +2 for border
		Render(logContent)
}

// Tea Commands
type repositoriesMsg struct {
	repositories []*repository.Repository
}

type cloningStartedMsg struct {
	progressTracker *cloning.ProgressTracker
}

type cloningProgressMsg struct {
	progress *cloning.Progress
}

type errorMsg struct {
	err error
}

type logUpdateMsg struct{}

func logUpdateCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
		return logUpdateMsg{}
	})
}

func fetchRepositoriesCmd(app *Application, config *Config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		filter := repository.NewRepositoryFilter()
		filter.IncludeForks = !config.SkipForks

		req := &usecases.FetchRepositoriesRequest{
			Owner:  config.Owner,
			Type:   config.RepoType,
			Filter: filter,
		}

		resp, err := app.fetchRepositoriesUseCase.Execute(ctx, req)
		if err != nil {
			return errorMsg{err: err}
		}

		return repositoriesMsg{repositories: resp.Repositories}
	}
}

func startCloningCmd(app *Application, repos []*repository.Repository, baseDir string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Convert relative path to absolute path
		absBaseDir := baseDir
		if !filepath.IsAbs(baseDir) {
			// If it's a relative path, make it relative to current working directory
			cwd, err := os.Getwd()
			if err != nil {
				app.logger.Error("Failed to get working directory", shared.ErrorField(err))
				return errorMsg{err: fmt.Errorf("failed to get working directory: %w", err)}
			}
			absBaseDir = filepath.Join(cwd, baseDir)
		}

		req := &usecases.CloneRepositoriesRequest{
			Repositories:  repos,
			BaseDirectory: absBaseDir,
			Options:       cloning.NewDefaultCloneOptions(),
			Concurrency:   runtime.NumCPU() * 2,
		}

		// Start cloning in background
		go func() {
			_, err := app.cloneRepositoriesUseCase.Execute(ctx, req)
			if err != nil {
				app.logger.Error("Cloning failed", shared.ErrorField(err))
			}
		}()

		// Return a message that starts progress tracking
		return cloningStartedMsg{
			progressTracker: nil, // Will be populated by real progress updates
		}
	}
}

func progressTickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		// This will be populated with actual progress when cloning starts
		return cloningProgressMsg{progress: nil}
	})
}

func realProgressTickCmd(app *Application) tea.Cmd {
	return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
		if app != nil && app.cloneRepositoriesUseCase != nil {
			progress := app.cloneRepositoriesUseCase.GetProgress()
			// Return progress even if nil - the TUI will handle it appropriately
			return cloningProgressMsg{progress: progress}
		}
		return cloningProgressMsg{progress: nil}
	})
}

func main() {
	// Parse command line arguments
	config, err := NewConfigFromArgs(os.Args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("Usage: ghclone <type> <owner> [token]")
		fmt.Println("  <type>: 'users' for user or 'orgs' for organization")
		fmt.Println("  <owner>: name of the user or organization")
		fmt.Println("  [token]: (optional) GitHub personal access token")
		os.Exit(1)
	}

	// Initialize application
	app, tuiLogger, err := NewApplication(config)
	if err != nil {
		fmt.Printf("Failed to initialize application: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	// Show configuration info (these go to stdout before TUI starts)
	fmt.Printf("ghclone v2.0 - Concurrent GitHub Repository Cloner\n")
	fmt.Printf("Target: %s/%s\n", config.RepoType, config.Owner)
	fmt.Printf("Concurrency: %d workers\n", runtime.NumCPU()*2)
	fmt.Printf("Log file: %s\n", tuiLogger.GetLogFile())
	if config.Token == "" {
		fmt.Printf("Warning: Running without GitHub token (rate limiting may apply)\n")
	}
	fmt.Printf("Starting...\n\n")

	// Start TUI
	model := newTUIModel(app, config, tuiLogger)
	p := tea.NewProgram(model)
	
	if _, err := p.Run(); err != nil {
		app.logger.Error("TUI failed", shared.ErrorField(err))
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}