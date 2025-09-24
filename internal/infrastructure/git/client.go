package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/italoag/repocloner/internal/domain/cloning"
	"github.com/italoag/repocloner/internal/domain/shared"
)

// GitClient handles Git operations
type GitClient struct {
	gitPath   string
	timeout   time.Duration
	logger    shared.Logger
	validator *GitValidator
}

// GitClientConfig holds configuration for Git client
type GitClientConfig struct {
	GitPath string
	Timeout time.Duration
	Logger  shared.Logger
}

// NewGitClient creates a new Git client
func NewGitClient(config *GitClientConfig) (*GitClient, error) {
	if config.GitPath == "" {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			return nil, fmt.Errorf("git not found in PATH: %w", err)
		}
		config.GitPath = gitPath
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute // Default timeout for clone operations
	}

	validator := NewGitValidator(config.Logger)

	return &GitClient{
		gitPath:   config.GitPath,
		timeout:   config.Timeout,
		logger:    config.Logger,
		validator: validator,
	}, nil
}

// CloneRepository clones a repository according to the job specifications
func (g *GitClient) CloneRepository(ctx context.Context, job *cloning.CloneJob) error {
	if err := g.validator.ValidateCloneJob(job); err != nil {
		return fmt.Errorf("invalid clone job: %w", err)
	}

	destPath := job.GetDestinationPath()

	// Check if repository already exists and handle accordingly
	if g.repositoryExists(destPath) {
		if job.Options.SkipExisting {
			g.logger.Info("Repository already exists, skipping",
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.StringField("path", destPath))
			return &RepositoryExistsError{Path: destPath}
		}

		// Remove existing directory if not skipping
		if err := os.RemoveAll(destPath); err != nil {
			return fmt.Errorf("failed to remove existing repository: %w", err)
		}
	}

	// Prepare destination directory
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Build git clone command
	args := g.buildCloneArgs(job)

	// Create context with timeout
	cloneCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Execute git clone
	cmd := exec.CommandContext(cloneCtx, g.gitPath, args...)
	cmd.Dir = filepath.Dir(destPath)

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		g.logger.Error("Git clone failed",
			shared.StringField("repo", job.Repository.GetFullName()),
			shared.StringField("output", string(output)),
			shared.ErrorField(err))

		// Parse git errors for better error messages
		return g.parseGitError(err, string(output))
	}

	g.logger.Info("Repository cloned successfully",
		shared.StringField("repo", job.Repository.GetFullName()),
		shared.StringField("path", destPath),
		shared.DurationField("duration", job.Duration()))

	return nil
}

// buildCloneArgs builds the arguments for git clone command
func (g *GitClient) buildCloneArgs(job *cloning.CloneJob) []string {
	args := []string{"clone"}

	// Add depth if specified (shallow clone)
	if job.Options.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", job.Options.Depth))
	}

	// Add branch if specified
	if job.Options.Branch != "" {
		args = append(args, "--branch", job.Options.Branch)
	}

	// Add recurse submodules if specified
	if job.Options.RecurseSubmodules {
		args = append(args, "--recurse-submodules")
	}

	// Add other useful options
	args = append(args, "--no-hardlinks") // Don't use hardlinks
	args = append(args, "--quiet")        // Minimize output

	// Add URL and destination
	args = append(args, job.Repository.CloneURL, job.GetDestinationPath())

	return args
}

// repositoryExists checks if a repository already exists at the given path
func (g *GitClient) repositoryExists(path string) bool {
	gitDir := filepath.Join(path, ".git")
	if stat, err := os.Stat(gitDir); err == nil {
		return stat.IsDir()
	}
	return false
}

// parseGitError parses git command errors and returns appropriate error types
func (g *GitClient) parseGitError(err error, output string) error {
	output = strings.ToLower(output)

	switch {
	case strings.Contains(output, "authentication failed"):
		return &AuthenticationError{Message: "Git authentication failed"}
	case strings.Contains(output, "repository not found"):
		return &RepositoryNotFoundError{Message: "Repository not found"}
	case strings.Contains(output, "permission denied"):
		return &PermissionError{Message: "Permission denied"}
	case strings.Contains(output, "network is unreachable"):
		return &NetworkError{Message: "Network unreachable"}
	case strings.Contains(output, "connection timed out"):
		return &TimeoutError{Message: "Connection timed out"}
	case strings.Contains(output, "no space left on device"):
		return &DiskSpaceError{Message: "No space left on device"}
	case strings.Contains(output, "filename too long"):
		return &PathTooLongError{Message: "File path too long"}
	default:
		return &GitError{
			Message: fmt.Sprintf("Git command failed: %v", err),
			Output:  output,
		}
	}
}

// ValidateGitInstallation checks if git is properly installed and accessible
func (g *GitClient) ValidateGitInstallation(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, g.gitPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	version := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "git version") {
		return fmt.Errorf("unexpected git version output: %s", version)
	}

	g.logger.Info("Git installation validated", shared.StringField("version", version))
	return nil
}

// GetRepositorySize estimates the size of a cloned repository
func (g *GitClient) GetRepositorySize(path string) (int64, error) {
	if !g.repositoryExists(path) {
		return 0, fmt.Errorf("repository does not exist at path: %s", path)
	}

	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// CleanupRepository removes a repository directory
func (g *GitClient) CleanupRepository(path string) error {
	if !g.repositoryExists(path) {
		return nil // Already clean
	}

	err := os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("failed to cleanup repository at %s: %w", path, err)
	}

	g.logger.Info("Repository cleaned up", shared.StringField("path", path))
	return nil
}

// CheckRepositoryIntegrity verifies that a cloned repository is valid
func (g *GitClient) CheckRepositoryIntegrity(ctx context.Context, path string) error {
	if !g.repositoryExists(path) {
		return fmt.Errorf("repository does not exist at path: %s", path)
	}

	// Run git fsck to check repository integrity
	cmd := exec.CommandContext(ctx, g.gitPath, "-C", path, "fsck", "--quick")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("repository integrity check failed: %w, output: %s", err, string(output))
	}

	return nil
}

// GetRemoteURL returns the remote URL of a repository
func (g *GitClient) GetRemoteURL(ctx context.Context, path string) (string, error) {
	if !g.repositoryExists(path) {
		return "", fmt.Errorf("repository does not exist at path: %s", path)
	}

	cmd := exec.CommandContext(ctx, g.gitPath, "-C", path, "config", "--get", "remote.origin.url")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// UpdateRepository pulls the latest changes from remote
func (g *GitClient) UpdateRepository(ctx context.Context, path string) error {
	if !g.repositoryExists(path) {
		return fmt.Errorf("repository does not exist at path: %s", path)
	}

	cmd := exec.CommandContext(ctx, g.gitPath, "-C", path, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to update repository: %w, output: %s", err, string(output))
	}

	g.logger.Info("Repository updated", shared.StringField("path", path))
	return nil
}
