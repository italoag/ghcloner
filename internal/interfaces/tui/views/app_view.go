package views

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/interfaces/tui/models"
)

// AppView handles rendering of the application UI
type AppView struct {
	model *models.AppModel
}

// NewAppView creates a new application view
func NewAppView(model *models.AppModel) *AppView {
	return &AppView{model: model}
}

// Render renders the complete application view
func (v *AppView) Render() string {
	return v.renderView()
}

// renderView renders the application view based on current state
func (v *AppView) renderView() string {
	if v.model.IsQuitting() {
		return v.renderQuitting()
	}

	switch v.model.GetState() {
	case models.StateInitializing:
		return v.renderInitializing()
	case models.StateFetchingRepositories:
		return v.renderFetching()
	case models.StateRepositoriesFetched:
		return v.renderRepositoriesFetched()
	case models.StateCloning:
		return v.renderCloning()
	case models.StateCloningComplete:
		return v.renderComplete()
	case models.StateError:
		return v.renderError()
	default:
		return v.renderUnknown()
	}
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginTop(1).
			MarginBottom(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginLeft(2)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFAF00")).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			MarginLeft(2).
			MarginBottom(1)

	progressStyle = lipgloss.NewStyle().
			MarginLeft(2).
			MarginTop(1).
			MarginBottom(1)

	statsStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2).
			MarginTop(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(2)
)

// renderInitializing renders the initializing state
func (v *AppView) renderInitializing() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚀 ghclone v0.2.0"),
		headerStyle.Render("Initializing Application"),
		infoStyle.Render("Setting up concurrent repository cloner..."),
		helpStyle.Render("Press 'q' to quit"),
	)
}

// renderFetching renders the repository fetching state
func (v *AppView) renderFetching() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚀 ghclone v0.2.0"),
		headerStyle.Render("Fetching Repositories"),
		infoStyle.Render("Retrieving repository list from GitHub..."),
		statusStyle.Render("⏳ Please wait while we fetch the repositories"),
		helpStyle.Render("Press 'q' to quit"),
	)
}

// renderRepositoriesFetched renders the state after repositories are fetched
func (v *AppView) renderRepositoriesFetched() string {
	count := v.model.GetRepositoryCount()

	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚀 ghclone v0.2.0"),
		headerStyle.Render("Repositories Found"),
		successStyle.Render(fmt.Sprintf("✓ Found %d repositories", count)),
		infoStyle.Render("Starting concurrent cloning..."),
		helpStyle.Render("Press 'q' to quit"),
	)
}

// renderCloning renders the cloning state with progress
func (v *AppView) renderCloning() string {
	progress := v.model.GetProgress()

	var content []string
	content = append(content, titleStyle.Render("🚀 ghclone v20..0"))
	content = append(content, headerStyle.Render("Cloning Repositories"))

	if progress != nil {
		// Progress bar
		progressBar := v.model.GetProgressView()
		content = append(content, progressStyle.Render(progressBar))

		// Statistics
		stats := v.renderProgressStats(progress)
		content = append(content, stats)

		// Current status
		status := v.renderCurrentStatus(progress)
		content = append(content, status)
	} else {
		content = append(content, infoStyle.Render("Initializing cloning process..."))
	}

	// Elapsed time
	elapsed := v.model.GetElapsedTime()
	if elapsed > 0 {
		content = append(content, infoStyle.Render(fmt.Sprintf("Elapsed: %v", elapsed.Truncate(time.Second))))
	}

	content = append(content, helpStyle.Render("Press 'q' to quit"))

	return lipgloss.JoinVertical(lipgloss.Left, content...)
}

// renderComplete renders the completion state
func (v *AppView) renderComplete() string {
	progress := v.model.GetProgress()
	elapsed := v.model.GetElapsedTime()

	var content []string
	content = append(content, titleStyle.Render("🚀 ghclone v0.2.0"))
	content = append(content, headerStyle.Render("Cloning Completed"))

	if progress != nil {
		// Success message
		content = append(content, successStyle.Render("✓ All repositories processed!"))

		// Final statistics
		stats := v.renderFinalStats(progress, elapsed)
		content = append(content, stats)

		// Success rate
		if progress.Failed > 0 {
			successRate := progress.GetSuccessRate()
			if successRate >= 90 {
				content = append(content, successStyle.Render(fmt.Sprintf("Success rate: %.1f%%", successRate)))
			} else if successRate >= 70 {
				content = append(content, warningStyle.Render(fmt.Sprintf("Success rate: %.1f%%", successRate)))
			} else {
				content = append(content, errorStyle.Render(fmt.Sprintf("Success rate: %.1f%%", successRate)))
			}
		}
	}

	content = append(content, helpStyle.Render("Press 'q' to quit, 'r' to restart"))

	return lipgloss.JoinVertical(lipgloss.Left, content...)
}

// renderError renders the error state
func (v *AppView) renderError() string {
	err := v.model.GetError()

	var content []string
	content = append(content, titleStyle.Render("🚀 ghclone v0.2.0"))
	content = append(content, headerStyle.Render("Error"))

	if err != nil {
		content = append(content, errorStyle.Render("✗ "+err.Error()))

		// Specific error handling
		switch err.(type) {
		case *models.NoRepositoriesError:
			content = append(content, infoStyle.Render("Possible reasons:"))
			content = append(content, infoStyle.Render("• User/organization doesn't exist"))
			content = append(content, infoStyle.Render("• No public repositories available"))
			content = append(content, infoStyle.Render("• Authentication required (set GITHUB_TOKEN)"))
		default:
			content = append(content, infoStyle.Render("Please check your configuration and try again"))
		}
	}

	content = append(content, helpStyle.Render("Press 'q' to quit, 'r' to restart"))

	return lipgloss.JoinVertical(lipgloss.Left, content...)
}

// renderQuitting renders the quitting state
func (v *AppView) renderQuitting() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚀 ghclone v0.2.0"),
		headerStyle.Render("Shutting Down"),
		infoStyle.Render("Thanks for using ghclone!"),
		infoStyle.Render("Cleaning up resources..."),
	)
}

// renderUnknown renders unknown states
func (v *AppView) renderUnknown() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚀 ghclone v0.2.0"),
		errorStyle.Render("Unknown state"),
		helpStyle.Render("Press 'q' to quit"),
	)
}

// renderProgressStats renders progress statistics
func (v *AppView) renderProgressStats(progress *cloning.Progress) string {
	stats := fmt.Sprintf(
		"Progress: %d/%d repositories\n"+
			"✓ Completed: %d\n"+
			"✗ Failed: %d\n"+
			"⏭ Skipped: %d\n"+
			"⏳ In Progress: %d",
		progress.Completed+progress.Failed+progress.Skipped,
		progress.Total,
		progress.Completed,
		progress.Failed,
		progress.Skipped,
		progress.InProgress,
	)

	return statsStyle.Render(stats)
}

// renderCurrentStatus renders current cloning status
func (v *AppView) renderCurrentStatus(progress *cloning.Progress) string {
	percentage := progress.GetPercentage()

	status := fmt.Sprintf("%.1f%% complete", percentage)

	if progress.ETA > 0 {
		status += fmt.Sprintf(" • ETA: %v", progress.ETA.Truncate(time.Second))
	}

	if progress.Throughput > 0 {
		status += fmt.Sprintf(" • %.1f repos/sec", progress.Throughput)
	}

	return statusStyle.Render(status)
}

// renderFinalStats renders final completion statistics
func (v *AppView) renderFinalStats(progress *cloning.Progress, elapsed time.Duration) string {
	stats := fmt.Sprintf(
		"Final Results:\n"+
			"📊 Total Repositories: %d\n"+
			"✅ Successfully Cloned: %d\n"+
			"❌ Failed: %d\n"+
			"⏭️ Skipped (already existed): %d\n"+
			"⏱️ Total Time: %v",
		progress.Total,
		progress.Completed,
		progress.Failed,
		progress.Skipped,
		elapsed.Truncate(time.Second),
	)

	// Add average time per repository
	if progress.Completed > 0 && elapsed > 0 {
		avgTime := elapsed / time.Duration(progress.Completed)
		stats += fmt.Sprintf("\n⚡ Average Time per Repository: %v", avgTime.Truncate(time.Millisecond*100))
	}

	return statsStyle.Render(stats)
}


