package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/italoag/ghcloner/internal/application/services"
	"github.com/italoag/ghcloner/internal/application/usecases"
	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
)

// AppModel represents the main application model
type AppModel struct {
	// Dependencies
	fetchUseCase     *usecases.FetchRepositoriesUseCase
	cloningService   *services.CloningService
	progressService  *services.ProgressService
	logger           shared.Logger

	// Configuration
	config *AppConfig

	// State
	state        AppState
	repositories []*repository.Repository
	progress     progress.Model
	currentBatch string
	
	// UI State
	quitting     bool
	err          error
	statusMsg    string
	
	// Progress tracking
	progressData *cloning.Progress
	startTime    time.Time
}

// AppConfig holds application configuration
type AppConfig struct {
	Owner       string
	RepoType    repository.RepositoryType
	Token       string
	BaseDir     string
	Concurrency int
	SkipForks   bool
	LogLevel    string
}

// AppState represents the current state of the application
type AppState int

const (
	StateInitializing AppState = iota
	StateFetchingRepositories
	StateRepositoriesFetched
	StateCloning
	StateCloningComplete
	StateError
	StateQuitting
)

// String returns the string representation of the app state
func (s AppState) String() string {
	switch s {
	case StateInitializing:
		return "Initializing"
	case StateFetchingRepositories:
		return "Fetching Repositories"
	case StateRepositoriesFetched:
		return "Repositories Fetched"
	case StateCloning:
		return "Cloning"
	case StateCloningComplete:
		return "Cloning Complete"
	case StateError:
		return "Error"
	case StateQuitting:
		return "Quitting"
	default:
		return "Unknown"
	}
}

// NewAppModel creates a new application model
func NewAppModel(
	config *AppConfig,
	fetchUseCase *usecases.FetchRepositoriesUseCase,
	cloningService *services.CloningService,
	progressService *services.ProgressService,
	logger shared.Logger,
) *AppModel {
	return &AppModel{
		fetchUseCase:    fetchUseCase,
		cloningService:  cloningService,
		progressService: progressService,
		logger:          logger.With(shared.StringField("component", "tui")),
		config:          config,
		state:           StateInitializing,
		progress:        progress.New(progress.WithDefaultGradient()),
		statusMsg:       "Initializing application...",
	}
}

// Init initializes the model
func (m *AppModel) Init() tea.Cmd {
	m.logger.Info("TUI application starting",
		shared.StringField("owner", m.config.Owner),
		shared.StringField("type", m.config.RepoType.String()))

	return tea.Batch(
		m.fetchRepositoriesCmd(),
		m.progressTickCmd(),
	)
}

// Update handles messages and updates the model
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case repositoriesFetchedMsg:
		return m.handleRepositoriesFetched(msg)

	case cloningStartedMsg:
		return m.handleCloningStarted(msg)

	case progressUpdateMsg:
		return m.handleProgressUpdate(msg)

	case cloningCompleteMsg:
		return m.handleCloningComplete(msg)

	case errorMsg:
		return m.handleError(msg)

	case progress.FrameMsg:
		var cmd tea.Cmd
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	default:
		return m, nil
	}
}

// handleKeyPress handles keyboard input
func (m *AppModel) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.logger.Info("User requested quit")
		m.state = StateQuitting
		m.quitting = true
		return m, tea.Quit
	case "r":
		if m.state == StateError || m.state == StateCloningComplete {
			m.logger.Info("User requested restart")
			return m.restart()
		}
	}
	return m, nil
}

// handleRepositoriesFetched handles the repositories fetched message
func (m *AppModel) handleRepositoriesFetched(msg repositoriesFetchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.logger.Error("Failed to fetch repositories", shared.ErrorField(msg.err))
		return m.handleError(errorMsg{err: msg.err})
	}

	m.repositories = msg.repositories
	m.state = StateRepositoriesFetched
	m.statusMsg = "Repositories fetched successfully"

	if len(m.repositories) == 0 {
		m.state = StateError
		m.err = NewNoRepositoriesError(m.config.Owner, m.config.RepoType)
		return m, nil
	}

	m.logger.Info("Repositories fetched",
		shared.IntField("count", len(m.repositories)))

	// Start cloning automatically
	return m.startCloning()
}

// handleCloningStarted handles the cloning started message
func (m *AppModel) handleCloningStarted(msg cloningStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.logger.Error("Failed to start cloning", shared.ErrorField(msg.err))
		return m.handleError(errorMsg{err: msg.err})
	}

	m.state = StateCloning
	m.currentBatch = msg.batchID
	m.startTime = time.Now()
	m.statusMsg = "Cloning repositories concurrently..."

	m.logger.Info("Cloning started",
		shared.StringField("batch_id", msg.batchID),
		shared.IntField("repository_count", len(m.repositories)))

	return m, m.progressTickCmd()
}

// handleProgressUpdate handles progress update messages
func (m *AppModel) handleProgressUpdate(msg progressUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.progress != nil {
		m.progressData = msg.progress
		
		// Update progress bar
		if m.progressData.Total > 0 {
			percentage := float64(m.progressData.Completed+m.progressData.Failed+m.progressData.Skipped) / float64(m.progressData.Total)
			cmd := m.progress.SetPercent(percentage)
			
			// Check if cloning is complete
			if m.progressData.IsComplete() {
				m.logger.Info("Cloning completed",
					shared.IntField("completed", m.progressData.Completed),
					shared.IntField("failed", m.progressData.Failed),
					shared.IntField("skipped", m.progressData.Skipped))

				return m.handleCloningComplete(cloningCompleteMsg{
					progress: m.progressData,
				})
			}
			
			return m, tea.Batch(cmd, m.progressTickCmd())
		}
	}

	return m, m.progressTickCmd()
}

// handleCloningComplete handles cloning completion
func (m *AppModel) handleCloningComplete(msg cloningCompleteMsg) (tea.Model, tea.Cmd) {
	m.state = StateCloningComplete
	m.progressData = msg.progress
	m.statusMsg = "Cloning completed successfully!"

	// Set progress to 100%
	cmd := m.progress.SetPercent(1.0)

	return m, cmd
}

// handleError handles error messages
func (m *AppModel) handleError(msg errorMsg) (tea.Model, tea.Cmd) {
	m.state = StateError
	m.err = msg.err
	m.statusMsg = "An error occurred"

	m.logger.Error("TUI error", shared.ErrorField(msg.err))

	return m, nil
}

// startCloning starts the cloning process
func (m *AppModel) startCloning() (tea.Model, tea.Cmd) {
	m.state = StateCloning
	m.statusMsg = "Starting concurrent cloning..."

	return m, m.startCloningCmd()
}

// restart restarts the application
func (m *AppModel) restart() (tea.Model, tea.Cmd) {
	m.state = StateInitializing
	m.repositories = nil
	m.progressData = nil
	m.currentBatch = ""
	m.err = nil
	m.quitting = false
	m.statusMsg = "Restarting..."
	m.progress = progress.New(progress.WithDefaultGradient())

	m.logger.Info("Application restarting")

	return m, m.fetchRepositoriesCmd()
}

// Commands

// fetchRepositoriesCmd creates a command to fetch repositories
func (m *AppModel) fetchRepositoriesCmd() tea.Cmd {
	return func() tea.Msg {
		m.state = StateFetchingRepositories

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		filter := repository.NewRepositoryFilter()
		filter.IncludeForks = !m.config.SkipForks

		req := &usecases.FetchRepositoriesRequest{
			Owner:  m.config.Owner,
			Type:   m.config.RepoType,
			Filter: filter,
		}

		resp, err := m.fetchUseCase.Execute(ctx, req)
		if err != nil {
			return repositoriesFetchedMsg{err: err}
		}

		return repositoriesFetchedMsg{
			repositories: resp.Repositories,
		}
	}
}

// startCloningCmd creates a command to start cloning
func (m *AppModel) startCloningCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		cloneOptions := &cloning.CloneOptions{
			Depth:             1,
			RecurseSubmodules: true,
			SkipExisting:      true,
			CreateOrgDirs:     false,
		}

		req := &services.CloneBatchRequest{
			Repositories:  m.repositories,
			BaseDirectory: m.config.BaseDir,
			Options:       cloneOptions,
		}

		resp, err := m.cloningService.CloneBatch(ctx, req)
		if err != nil {
			return cloningStartedMsg{err: err}
		}

		return cloningStartedMsg{
			batchID: resp.BatchID,
		}
	}
}

// progressTickCmd creates a command to tick progress updates
func (m *AppModel) progressTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		if m.state != StateCloning {
			return nil
		}

		progress := m.cloningService.GetProgress()
		return progressUpdateMsg{progress: progress}
	})
}

// Messages

// repositoriesFetchedMsg is sent when repositories are fetched
type repositoriesFetchedMsg struct {
	repositories []*repository.Repository
	err          error
}

// cloningStartedMsg is sent when cloning starts
type cloningStartedMsg struct {
	batchID string
	err     error
}

// progressUpdateMsg is sent with progress updates
type progressUpdateMsg struct {
	progress *cloning.Progress
}

// cloningCompleteMsg is sent when cloning is complete
type cloningCompleteMsg struct {
	progress *cloning.Progress
}

// errorMsg is sent when an error occurs
type errorMsg struct {
	err error
}

// Custom Errors

// NoRepositoriesError represents an error when no repositories are found
type NoRepositoriesError struct {
	Owner string
	Type  repository.RepositoryType
}

// NewNoRepositoriesError creates a new NoRepositoriesError
func NewNoRepositoriesError(owner string, repoType repository.RepositoryType) *NoRepositoriesError {
	return &NoRepositoriesError{
		Owner: owner,
		Type:  repoType,
	}
}

// Error returns the error message
func (e *NoRepositoriesError) Error() string {
	return "no repositories found for " + e.Type.String() + "/" + e.Owner
}

// Getters

// GetState returns the current application state
func (m *AppModel) GetState() AppState {
	return m.state
}

// GetRepositoryCount returns the number of repositories
func (m *AppModel) GetRepositoryCount() int {
	return len(m.repositories)
}

// GetProgress returns the current progress data
func (m *AppModel) GetProgress() *cloning.Progress {
	return m.progressData
}

// IsQuitting returns whether the application is quitting
func (m *AppModel) IsQuitting() bool {
	return m.quitting
}

// GetError returns the current error
func (m *AppModel) GetError() error {
	return m.err
}

// GetStatusMessage returns the current status message
func (m *AppModel) GetStatusMessage() string {
	return m.statusMsg
}

// GetElapsedTime returns the elapsed time since cloning started
func (m *AppModel) GetElapsedTime() time.Duration {
	if m.startTime.IsZero() {
		return 0
	}
	return time.Since(m.startTime)
}

// GetProgressView returns the progress bar view
func (m *AppModel) GetProgressView() string {
	return m.progress.View()
}

// View renders the model (implements tea.Model interface)
func (m *AppModel) View() string {
	// Import would cause circular dependency, so provide simple view
	if m.quitting {
		return "Thanks for using ghclone! Shutting down...\n"
	}
	
	var content []string
	content = append(content, "🚀 ghclone v2.0")
	content = append(content, "State: "+m.state.String())
	content = append(content, "Status: "+m.statusMsg)
	
	if m.progressData != nil {
		content = append(content, fmt.Sprintf("Progress: %d/%d completed, %d failed, %d skipped",
			m.progressData.Completed, m.progressData.Total, m.progressData.Failed, m.progressData.Skipped))
	}
	
	if m.err != nil {
		content = append(content, "Error: "+m.err.Error())
	}
	
	content = append(content, "\nPress 'q' to quit")
	
	return strings.Join(content, "\n")
}