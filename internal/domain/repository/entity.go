package repository

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// RepositoryID represents a unique identifier for a repository
type RepositoryID int64

// Repository represents a GitHub repository entity
type Repository struct {
	ID            RepositoryID `json:"id"`
	Name          string       `json:"name"`
	CloneURL      string       `json:"clone_url"`
	Owner         string       `json:"owner"`
	IsFork        bool         `json:"fork"`
	Size          int64        `json:"size"`
	DefaultBranch string       `json:"default_branch"`
	Language      string       `json:"language,omitempty"`
	Description   string       `json:"description,omitempty"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// NewRepository creates a new repository with validation
func NewRepository(
	id RepositoryID,
	name, cloneURL, owner string,
	isFork bool,
	size int64,
	defaultBranch string,
) (*Repository, error) {
	repo := &Repository{
		ID:            id,
		Name:          name,
		CloneURL:      cloneURL,
		Owner:         owner,
		IsFork:        isFork,
		Size:          size,
		DefaultBranch: defaultBranch,
		UpdatedAt:     time.Now(),
	}

	if err := repo.Validate(); err != nil {
		return nil, fmt.Errorf("invalid repository: %w", err)
	}

	return repo, nil
}

// Validate ensures the repository has valid data
func (r *Repository) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("repository name cannot be empty")
	}

	if r.Owner == "" {
		return fmt.Errorf("repository owner cannot be empty")
	}

	if err := r.ValidateCloneURL(); err != nil {
		return fmt.Errorf("invalid clone URL: %w", err)
	}

	if r.Size < 0 {
		return fmt.Errorf("repository size cannot be negative")
	}

	return nil
}

// ValidateCloneURL checks if the clone URL is valid
func (r *Repository) ValidateCloneURL() error {
	if r.CloneURL == "" {
		return fmt.Errorf("clone URL cannot be empty")
	}

	parsedURL, err := url.Parse(r.CloneURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "https" && parsedURL.Scheme != "ssh" {
		return fmt.Errorf("clone URL must use https or ssh protocol")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("clone URL must have a valid host")
	}

	return nil
}

// GetLocalPath returns the local path where the repository should be cloned
func (r *Repository) GetLocalPath(baseDir string) string {
	return filepath.Join(baseDir, r.Name)
}

// GetFullName returns the full name of the repository (owner/name)
func (r *Repository) GetFullName() string {
	return fmt.Sprintf("%s/%s", r.Owner, r.Name)
}

// IsPublic checks if the repository is public based on clone URL
func (r *Repository) IsPublic() bool {
	return strings.HasPrefix(r.CloneURL, "https://")
}

// String returns a string representation of the repository
func (r *Repository) String() string {
	return r.GetFullName()
}

// Equal checks if two repositories are equal
func (r *Repository) Equal(other *Repository) bool {
	if other == nil {
		return false
	}
	return r.ID == other.ID && r.CloneURL == other.CloneURL
}
