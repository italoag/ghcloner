package fang

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
	"github.com/spf13/cobra"

	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

// CloneConfig holds clone command configuration
type CloneConfig struct {
	Type      repository.RepositoryType
	Owner     string
	SkipForks bool
	Depth     int
	Branch    string
}

// NewCloneCommand creates the clone subcommand
func NewCloneCommand() *cobra.Command {
	var cloneConfig CloneConfig

	cmd := &cobra.Command{
		Use:   "clone [type] [owner]",
		Short: "Clone repositories from a GitHub user or organization",
		Long: `Clone repositories concurrently from a GitHub user or organization.

The clone command fetches all repositories from the specified user or organization
and clones them concurrently to the local filesystem. It provides real-time
progress tracking with an interactive terminal UI showing completion status,
throughput metrics, and detailed logging.

Repository Types:
  user, users         Clone from a GitHub user account
  org, orgs           Clone from a GitHub organization

The command supports advanced filtering options, configurable concurrency,
and comprehensive error handling with detailed progress reporting.`,
		Example: `  # Clone all repositories from a user
  ghclone clone user octocat

  # Clone organization repositories skipping forks
  ghclone clone org microsoft --skip-forks

  # Clone with custom concurrency and depth
  ghclone clone user torvalds --concurrency 8 --depth 5

  # Clone specific branch with custom base directory
  ghclone clone org kubernetes --branch main --base-dir /tmp/repos`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloneCommand(cmd, args, &cloneConfig)
		},
	}

	// Command-specific flags
	cmd.Flags().BoolVar(&cloneConfig.SkipForks, "skip-forks", true, "Skip forked repositories")
	cmd.Flags().Bool("include-forks", false, "Include forked repositories (inverse of --skip-forks)")
	cmd.Flags().IntVar(&cloneConfig.Depth, "depth", 1, "Clone depth for shallow clones (0 for full history)")
	cmd.Flags().StringVar(&cloneConfig.Branch, "branch", "", "Specific branch to clone (default: repository default branch)")

	return cmd
}

// runCloneCommand executes the clone command logic
func runCloneCommand(cmd *cobra.Command, args []string, cloneConfig *CloneConfig) error {
	// Parse and validate arguments
	typeStr := strings.ToLower(args[0])
	owner := args[1]

	switch typeStr {
	case "user", "users":
		cloneConfig.Type = repository.RepositoryTypeUser
	case "org", "orgs", "organization":
		cloneConfig.Type = repository.RepositoryTypeOrganization
	default:
		return fmt.Errorf("invalid repository type '%s', must be 'user' or 'org'", typeStr)
	}

	cloneConfig.Owner = owner

	// Handle include-forks flag (inverse of skip-forks)
	if includeForks, _ := cmd.Flags().GetBool("include-forks"); includeForks {
		cloneConfig.SkipForks = false
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

	// Initialize application
	app, tuiLogger, err := NewApplication(globalConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			app.logger.Warn("failed to close application", shared.ErrorField(err))
		}
	}()

	// Show configuration info before starting TUI
	fmt.Printf("ghclone v0.2.0 - Concurrent GitHub Repository Cloner\n")
	fmt.Printf("Target: %s/%s\n", cloneConfig.Type, cloneConfig.Owner)
	fmt.Printf("Concurrency: %d workers\n", globalConfig.Concurrency)
	fmt.Printf("Base directory: %s\n", globalConfig.BaseDir)
	fmt.Printf("Log file: %s\n", tuiLogger.GetLogFile())
	if globalConfig.Token == "" {
		fmt.Printf("Warning: Running without GitHub token (rate limiting may apply)\n")
	}
	if cloneConfig.SkipForks {
		fmt.Printf("Skipping forked repositories\n")
	}
	fmt.Printf("Starting...\n\n")

	// Create destination directory
	destDir := filepath.Join(globalConfig.BaseDir, cloneConfig.Owner)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Start TUI
	model := newCloneTUIModel(app, cloneConfig, globalConfig, tuiLogger)
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		app.logger.Error("TUI failed", shared.ErrorField(err))
		return fmt.Errorf("TUI execution failed: %w", err)
	}

	return nil
}

// TUI Model for clone command
type cloneTUIModel struct {
	app             *Application
	cloneConfig     *CloneConfig
	globalConfig    *Config
	repos           []*repository.Repository
	total           int
	progress        progress.Model
	quitting        bool
	err             error
	tuiLogger       *logging.TUILogger
	logHeight       int
	showLogs        bool
	actualProgress  *cloning.Progress // Store actual progress for display
}

func newCloneTUIModel(app *Application, cloneConfig *CloneConfig, globalConfig *Config, tuiLogger *logging.TUILogger) cloneTUIModel {
	return cloneTUIModel{
		app:          app,
		cloneConfig:  cloneConfig,
		globalConfig: globalConfig,
		progress:     progress.New(progress.WithDefaultGradient()),
		tuiLogger:    tuiLogger,
		logHeight:    8, // Show last 8 log entries
		showLogs:     true,
	}
}

func (m cloneTUIModel) Init() tea.Cmd {
	return fetchRepositoriesCmd(m.app, m.cloneConfig)
}

func (m cloneTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.err = fmt.Errorf("no repositories found for %s/%s", m.cloneConfig.Type, m.cloneConfig.Owner)
			m.quitting = true
			return m, tea.Quit
		}

		// Start concurrent cloning
		return m, startCloningCmd(m.app, m.repos, m.globalConfig.BaseDir, m.cloneConfig)

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
}

func (m cloneTUIModel) View() string {
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
		completionMsg.WriteString(fmt.Sprintf("📁 Directory: %s\n", filepath.Join(m.globalConfig.BaseDir, m.cloneConfig.Owner)))

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
		Render("🚀 ghclone v0.2.0 - Concurrent GitHub Repository Cloner")

	// Progress info
	info := fmt.Sprintf("Cloning repositories to '%s' directory...", filepath.Join(m.globalConfig.BaseDir, m.cloneConfig.Owner))
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
func (m cloneTUIModel) renderProgressDetails() string {
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
func (m cloneTUIModel) renderRecentCompletion() string {
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

// renderLogs renders the log display area
func (m cloneTUIModel) renderLogs() string {
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

func fetchRepositoriesCmd(app *Application, config *CloneConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		filter := repository.NewRepositoryFilter()
		filter.IncludeForks = !config.SkipForks

		req := &usecases.FetchRepositoriesRequest{
			Owner:  config.Owner,
			Type:   config.Type,
			Filter: filter,
		}

		resp, err := app.fetchRepositoriesUseCase.Execute(ctx, req)
		if err != nil {
			return errorMsg{err: err}
		}

		return repositoriesMsg{repositories: resp.Repositories}
	}
}

func startCloningCmd(app *Application, repos []*repository.Repository, baseDir string, config *CloneConfig) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Create destination directory
		destDir := filepath.Join(baseDir, config.Owner)

		req := &usecases.CloneRepositoriesRequest{
			Repositories:  repos,
			BaseDirectory: destDir,
			Options:       createCloneOptions(config),
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

// createCloneOptions creates clone options from the clone config
func createCloneOptions(config *CloneConfig) *cloning.CloneOptions {
	options := cloning.NewDefaultCloneOptions()
	options.Depth = config.Depth
	options.Branch = config.Branch
	options.SkipExisting = true
	options.CreateOrgDirs = false
	options.RecurseSubmodules = true
	return options
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
