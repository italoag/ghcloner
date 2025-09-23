package fang

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// BitbucketCloneConfig holds bitbucket clone command configuration
type BitbucketCloneConfig struct {
	Type      repository.RepositoryType
	Owner     string
	SkipForks bool
	Depth     int
	Branch    string
}

// NewBitbucketCloneCommand creates the bitbucket clone subcommand
func NewBitbucketCloneCommand() *cobra.Command {
	cloneConfig := &BitbucketCloneConfig{}

	cmd := &cobra.Command{
		Use:   "bitbucket [user|workspace] <owner>",
		Short: "Clone repositories from a Bitbucket user or workspace",
		Long: `Clone repositories from a Bitbucket user or workspace with concurrent processing.

Supports both individual users and workspaces. Uses Bitbucket's API v2.0 to fetch 
repository information and performs concurrent cloning with real-time progress tracking.

Authentication:
  Requires Bitbucket username and app password for private repositories.
  Set BITBUCKET_USERNAME and BITBUCKET_APP_PASSWORD environment variables.

Examples:
  # Clone all repositories from a user
  bitbucket clone user myusername

  # Clone workspace repositories with custom settings  
  bitbucket clone workspace myworkspace --concurrency 4 --skip-forks

  # Clone with specific depth and branch
  bitbucket clone user myusername --depth 5 --branch develop`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBitbucketCloneCommand(cmd, args, cloneConfig)
		},
	}

	// Command-specific flags
	cmd.Flags().BoolVar(&cloneConfig.SkipForks, "skip-forks", true, "Skip forked repositories")
	cmd.Flags().Bool("include-forks", false, "Include forked repositories (inverse of --skip-forks)")
	cmd.Flags().IntVar(&cloneConfig.Depth, "depth", 1, "Clone depth for shallow clones (0 for full history)")
	cmd.Flags().StringVar(&cloneConfig.Branch, "branch", "", "Specific branch to clone (default: repository default branch)")

	return cmd
}

// runBitbucketCloneCommand executes the bitbucket clone command logic
func runBitbucketCloneCommand(cmd *cobra.Command, args []string, cloneConfig *BitbucketCloneConfig) error {
	// Parse and validate arguments
	typeStr := strings.ToLower(args[0])
	owner := args[1]

	switch typeStr {
	case "user", "users":
		cloneConfig.Type = repository.RepositoryTypeBitbucketUser
	case "workspace", "workspaces", "ws":
		cloneConfig.Type = repository.RepositoryTypeBitbucketWorkspace
	default:
		return fmt.Errorf("invalid repository type '%s', must be 'user' or 'workspace'", typeStr)
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

	// Override Bitbucket credentials from environment if not set
	if globalConfig.BitbucketUsername == "" {
		globalConfig.BitbucketUsername = os.Getenv("BITBUCKET_USERNAME")
	}
	if globalConfig.BitbucketAppPassword == "" {
		globalConfig.BitbucketAppPassword = os.Getenv("BITBUCKET_APP_PASSWORD")
	}

	// Validate Bitbucket credentials are provided
	if globalConfig.BitbucketUsername == "" || globalConfig.BitbucketAppPassword == "" {
		return fmt.Errorf("Bitbucket credentials required: set BITBUCKET_USERNAME and BITBUCKET_APP_PASSWORD environment variables")
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

	// Create base directory if it doesn't exist
	baseDir, err := filepath.Abs(globalConfig.BaseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory: %w", err)
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Run TUI application
	tuiModel := newBitbucketCloneTUIModel(app, cloneConfig, baseDir, tuiLogger)
	program := tea.NewProgram(tuiModel, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI application failed: %w", err)
	}

	return nil
}

// bitbucketCloneTUIModel represents the TUI state for bitbucket cloning
type bitbucketCloneTUIModel struct {
	app          *Application
	config       *BitbucketCloneConfig
	baseDir      string
	logger       *logging.TUILogger
	progress     progress.Model
	state        string
	message      string
	logs         []string
	repositories []*repository.Repository
	done         bool
	err          error
}

// newBitbucketCloneTUIModel creates a new TUI model for bitbucket cloning
func newBitbucketCloneTUIModel(app *Application, config *BitbucketCloneConfig, baseDir string, logger *logging.TUILogger) *bitbucketCloneTUIModel {
	p := progress.New(progress.WithDefaultGradient())
	
	return &bitbucketCloneTUIModel{
		app:     app,
		config:  config,
		baseDir: baseDir,
		logger:  logger,
		progress: p,
		state:   "initializing",
		logs:    []string{},
	}
}

func (m *bitbucketCloneTUIModel) Init() tea.Cmd {
	return tea.Batch(
		m.progress.Init(),
		bitbucketFetchRepositoriesCmd(m.app, m.config),
		bitbucketProgressTickCmd(m.app),
	)
}

func (m *bitbucketCloneTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.done = true
			return m, tea.Quit
		}
		return m, nil

	case bitbucketRepositoriesFetchedMsg:
		m.repositories = msg.repositories
		m.state = "cloning"
		m.message = fmt.Sprintf("Found %d repositories. Starting clone...", len(msg.repositories))
		
		// Start cloning
		return m, bitbucketStartCloningCmd(m.app, m.repositories, m.baseDir, m.config)

	case bitbucketCloningProgressMsg:
		if msg.progress != nil {
			percent := float64(msg.progress.Completed) / float64(msg.progress.Total)
			m.message = fmt.Sprintf("Cloned %d/%d repositories", msg.progress.Completed, msg.progress.Total)
			return m, tea.Batch(
				m.progress.SetPercent(percent),
				bitbucketProgressTickCmd(m.app),
			)
		}
		return m, bitbucketProgressTickCmd(m.app)

	case bitbucketCloningCompletedMsg:
		m.state = "completed"
		m.message = fmt.Sprintf("Cloning completed! %d repositories processed", len(m.repositories))
		m.done = true
		return m, tea.Sequence(
			m.progress.SetPercent(1.0),
			tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tea.Quit() }),
		)

	case bitbucketErrorMsg:
		m.state = "error"
		m.message = fmt.Sprintf("Error: %s", msg.err.Error())
		m.err = msg.err
		m.done = true
		return m, tea.Sequence(
			tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tea.Quit() }),
		)

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m *bitbucketCloneTUIModel) View() string {
	if m.done && m.err != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.message + "\n")
	}

	var s strings.Builder
	s.WriteString(lipgloss.NewStyle().Bold(true).Render("Bitbucket Repository Cloner") + "\n\n")
	s.WriteString(fmt.Sprintf("Owner: %s (%s)\n", m.config.Owner, m.config.Type.String()))
	s.WriteString(fmt.Sprintf("Base Directory: %s\n", m.baseDir))
	s.WriteString(fmt.Sprintf("State: %s\n\n", m.state))
	
	if m.message != "" {
		s.WriteString(m.message + "\n\n")
	}
	
	if m.state == "cloning" {
		s.WriteString(m.progress.View() + "\n\n")
	}

	// Show recent logs
	logEntries := m.logger.GetLogBuffer().GetRecent(5)
	if len(logEntries) > 0 {
		s.WriteString("Recent logs:\n")
		for _, entry := range logEntries {
			s.WriteString(fmt.Sprintf("  [%s] %s\n", entry.Level, entry.Message))
		}
	}

	if !m.done {
		s.WriteString("\nPress 'q' or 'ctrl+c' to quit")
	}

	return s.String()
}

// Bitbucket-specific TUI message types
type bitbucketRepositoriesFetchedMsg struct {
	repositories []*repository.Repository
}

type bitbucketCloningProgressMsg struct {
	progress *cloning.Progress
}

type bitbucketCloningCompletedMsg struct{}

type bitbucketErrorMsg struct {
	err error
}

// bitbucketFetchRepositoriesCmd fetches repositories from Bitbucket
func bitbucketFetchRepositoriesCmd(app *Application, config *BitbucketCloneConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Create filter for repository fetching
		filter := repository.NewRepositoryFilter()
		filter.IncludeForks = !config.SkipForks

		fetchReq := &usecases.FetchRepositoriesRequest{
			Owner:  config.Owner,
			Type:   config.Type,
			Filter: filter,
		}

		fetchResp, err := app.fetchRepositoriesUseCase.Execute(ctx, fetchReq)
		if err != nil {
			return bitbucketErrorMsg{err: fmt.Errorf("failed to fetch repositories: %w", err)}
		}

		if len(fetchResp.Repositories) == 0 {
			return bitbucketErrorMsg{err: fmt.Errorf("no repositories found for %s/%s", config.Type, config.Owner)}
		}

		return bitbucketRepositoriesFetchedMsg{repositories: fetchResp.Repositories}
	}
}

// bitbucketStartCloningCmd starts the cloning process
func bitbucketStartCloningCmd(app *Application, repositories []*repository.Repository, baseDir string, config *BitbucketCloneConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Prepare clone options
		cloneOptions := &cloning.CloneOptions{
			Depth:             config.Depth,
			RecurseSubmodules: true,
			Branch:            config.Branch,
			SkipExisting:      true,
			CreateOrgDirs:     false,
		}

		// Create clone request
		cloneReq := &usecases.CloneRepositoriesRequest{
			Repositories:  repositories,
			BaseDirectory: baseDir,
			Options:       cloneOptions,
			Concurrency:   8, // Default concurrency
		}

		// Start cloning
		_, err := app.cloneRepositoriesUseCase.Execute(ctx, cloneReq)
		if err != nil {
			return bitbucketErrorMsg{err: fmt.Errorf("failed to clone repositories: %w", err)}
		}

		return bitbucketCloningCompletedMsg{}
	}
}

// bitbucketProgressTickCmd ticks progress updates
func bitbucketProgressTickCmd(app *Application) tea.Cmd {
	return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
		if app != nil && app.cloneRepositoriesUseCase != nil {
			progress := app.cloneRepositoriesUseCase.GetProgress()
			return bitbucketCloningProgressMsg{progress: progress}
		}
		return bitbucketCloningProgressMsg{progress: nil}
	})
}