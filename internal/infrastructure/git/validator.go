package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/shared"
)

// Git-specific errors
type GitError struct {
	Message string
	Output  string
}

func (e *GitError) Error() string {
	return e.Message
}

type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string {
	return e.Message
}

type RepositoryNotFoundError struct {
	Message string
}

func (e *RepositoryNotFoundError) Error() string {
	return e.Message
}

type RepositoryExistsError struct {
	Path string
}

func (e *RepositoryExistsError) Error() string {
	return fmt.Sprintf("repository already exists at: %s", e.Path)
}

type PermissionError struct {
	Message string
}

func (e *PermissionError) Error() string {
	return e.Message
}

type NetworkError struct {
	Message string
}

func (e *NetworkError) Error() string {
	return e.Message
}

type TimeoutError struct {
	Message string
}

func (e *TimeoutError) Error() string {
	return e.Message
}

type DiskSpaceError struct {
	Message string
}

func (e *DiskSpaceError) Error() string {
	return e.Message
}

type PathTooLongError struct {
	Message string
}

func (e *PathTooLongError) Error() string {
	return e.Message
}

// GitValidator validates Git operations and repository states
type GitValidator struct {
	logger shared.Logger
}

// NewGitValidator creates a new Git validator
func NewGitValidator(logger shared.Logger) *GitValidator {
	return &GitValidator{
		logger: logger,
	}
}

// ValidateCloneJob validates a clone job before execution
func (v *GitValidator) ValidateCloneJob(job *cloning.CloneJob) error {
	if job == nil {
		return fmt.Errorf("clone job cannot be nil")
	}

	if job.Repository == nil {
		return fmt.Errorf("repository cannot be nil")
	}

	// Validate repository clone URL
	if err := v.ValidateCloneURL(job.Repository.CloneURL); err != nil {
		return fmt.Errorf("invalid clone URL: %w", err)
	}

	// Validate destination path
	if err := v.ValidateDestinationPath(job.GetDestinationPath()); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Validate clone options
	if err := v.ValidateCloneOptions(job.Options); err != nil {
		return fmt.Errorf("invalid clone options: %w", err)
	}

	return nil
}

// ValidateCloneURL validates a Git clone URL
func (v *GitValidator) ValidateCloneURL(url string) error {
	if url == "" {
		return fmt.Errorf("clone URL cannot be empty")
	}

	// Valid URL patterns for Git
	patterns := []string{
		`^https://github\.com/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.git$`,
		`^git@github\.com:[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.git$`,
		`^https://gitlab\.com/[a-zA-Z0-9._/-]+\.git$`,
		`^git@gitlab\.com:[a-zA-Z0-9._/-]+\.git$`,
		`^https://bitbucket\.org/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.git$`,
		`^git@bitbucket\.org:[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.git$`,
	}

	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, url)
		if err != nil {
			continue
		}
		if matched {
			return nil
		}
	}

	// More flexible validation for other Git hosting services
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "git@") {
		if strings.HasSuffix(url, ".git") {
			return nil
		}
	}

	return fmt.Errorf("invalid or unsupported clone URL format: %s", url)
}

// ValidateDestinationPath validates the destination path for cloning
func (v *GitValidator) ValidateDestinationPath(path string) error {
	if path == "" {
		return fmt.Errorf("destination path cannot be empty")
	}

	// Check if path is absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("destination path must be absolute: %s", path)
	}

	// Validate path length (avoid issues on different filesystems)
	if len(path) > 260 { // Windows MAX_PATH limitation
		return fmt.Errorf("destination path too long (max 260 characters): %s", path)
	}

	// Check for invalid characters in path
	if err := v.validatePathCharacters(path); err != nil {
		return err
	}

	// Check parent directory exists and is writable
	parentDir := filepath.Dir(path)
	if err := v.validateParentDirectory(parentDir); err != nil {
		return err
	}

	// Check if path already exists and is not empty
	if stat, err := os.Stat(path); err == nil {
		if stat.IsDir() {
			if !v.isDirectoryEmpty(path) {
				// Directory exists and is not empty - this might be okay if we're updating
				v.logger.Warn("Destination directory exists and is not empty",
					shared.StringField("path", path))
			}
		} else {
			return fmt.Errorf("destination path exists but is not a directory: %s", path)
		}
	}

	return nil
}

// ValidateCloneOptions validates clone options
func (v *GitValidator) ValidateCloneOptions(options *cloning.CloneOptions) error {
	if options == nil {
		return fmt.Errorf("clone options cannot be nil")
	}

	if options.Depth < 0 {
		return fmt.Errorf("clone depth cannot be negative: %d", options.Depth)
	}

	if options.Branch != "" {
		// Validate branch name format
		if err := v.validateBranchName(options.Branch); err != nil {
			return fmt.Errorf("invalid branch name: %w", err)
		}
	}

	return nil
}

// validatePathCharacters checks for invalid characters in file paths
func (v *GitValidator) validatePathCharacters(path string) error {
	// Characters that are generally invalid in file paths
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	
	for _, char := range invalidChars {
		if strings.Contains(path, char) {
			return fmt.Errorf("path contains invalid character '%s': %s", char, path)
		}
	}

	// Check for control characters
	for _, r := range path {
		if r < 32 || r == 127 {
			return fmt.Errorf("path contains control character: %s", path)
		}
	}

	return nil
}

// validateParentDirectory checks if parent directory exists and is writable
// Creates the directory if it doesn't exist
func (v *GitValidator) validateParentDirectory(parentDir string) error {
	stat, err := os.Stat(parentDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Create the parent directory
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			// Re-stat to verify creation
			stat, err = os.Stat(parentDir)
			if err != nil {
				return fmt.Errorf("failed to verify created parent directory: %w", err)
			}
		} else {
			return fmt.Errorf("cannot access parent directory: %w", err)
		}
	}

	if !stat.IsDir() {
		return fmt.Errorf("parent path is not a directory: %s", parentDir)
	}

	// Test write permissions by trying to create a temporary file
	tempFile := filepath.Join(parentDir, ".ghclone_write_test")
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("parent directory is not writable: %w", err)
	}
	file.Close()
	os.Remove(tempFile)

	return nil
}

// isDirectoryEmpty checks if a directory is empty
func (v *GitValidator) isDirectoryEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

// validateBranchName validates Git branch name format
func (v *GitValidator) validateBranchName(branch string) error {
	if branch == "" {
		return fmt.Errorf("branch name cannot be empty")
	}

	// Git branch name rules
	invalidPatterns := []string{
		`^\.`,           // Cannot start with dot
		`\.\.$`,         // Cannot end with ..
		`@\{`,           // Cannot contain @{
		`\\$`,           // Cannot end with backslash
		`^/`,            // Cannot start with /
		`/$`,            // Cannot end with /
		`//`,            // Cannot contain consecutive /
		`[\x00-\x1f\x7f]`, // Cannot contain control characters
		`[ ]$`,          // Cannot end with space
		`^[ ]`,          // Cannot start with space
	}

	for _, pattern := range invalidPatterns {
		matched, err := regexp.MatchString(pattern, branch)
		if err != nil {
			continue
		}
		if matched {
			return fmt.Errorf("branch name violates Git naming rules: %s", branch)
		}
	}

	// Check for invalid characters
	invalidChars := []string{"~", "^", ":", "?", "*", "[", "\\"}
	for _, char := range invalidChars {
		if strings.Contains(branch, char) {
			return fmt.Errorf("branch name contains invalid character '%s': %s", char, branch)
		}
	}

	return nil
}

// ValidateGitRepository checks if a directory contains a valid Git repository
func (v *GitValidator) ValidateGitRepository(path string) error {
	gitDir := filepath.Join(path, ".git")
	
	stat, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not a git repository (missing .git directory): %s", path)
		}
		return fmt.Errorf("cannot access .git directory: %w", err)
	}

	if !stat.IsDir() {
		return fmt.Errorf(".git is not a directory: %s", path)
	}

	// Check for essential Git files
	essentialFiles := []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "config"),
	}

	for _, file := range essentialFiles {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("missing essential git file: %s", file)
		}
	}

	return nil
}

// IsRetryableError determines if a Git error is retryable
func (v *GitValidator) IsRetryableError(err error) bool {
	switch err.(type) {
	case *NetworkError, *TimeoutError:
		return true
	case *GitError:
		// Some git errors might be retryable
		gitErr := err.(*GitError)
		retryableMessages := []string{
			"connection reset",
			"temporary failure",
			"service unavailable",
			"try again",
		}
		
		for _, msg := range retryableMessages {
			if strings.Contains(strings.ToLower(gitErr.Output), msg) {
				return true
			}
		}
	}
	
	return false
}

// IsPermanentError determines if a Git error is permanent and shouldn't be retried
func (v *GitValidator) IsPermanentError(err error) bool {
	switch err.(type) {
	case *AuthenticationError, *RepositoryNotFoundError, *PermissionError, *DiskSpaceError, *PathTooLongError:
		return true
	}
	
	return false
}