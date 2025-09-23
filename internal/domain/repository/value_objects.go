package repository

import "time"

// RepositoryType represents the type of repository owner
type RepositoryType string

const (
	// GitHub repository types
	RepositoryTypeUser         RepositoryType = "users"
	RepositoryTypeOrganization RepositoryType = "orgs"
	
	// Bitbucket repository types
	RepositoryTypeBitbucketUser      RepositoryType = "bitbucket_users"
	RepositoryTypeBitbucketWorkspace RepositoryType = "bitbucket_workspaces"
)

// IsValid checks if the repository type is valid
func (rt RepositoryType) IsValid() bool {
	return rt == RepositoryTypeUser || 
		rt == RepositoryTypeOrganization || 
		rt == RepositoryTypeBitbucketUser || 
		rt == RepositoryTypeBitbucketWorkspace
}

// IsGitHubType checks if the repository type is for GitHub
func (rt RepositoryType) IsGitHubType() bool {
	return rt == RepositoryTypeUser || rt == RepositoryTypeOrganization
}

// IsBitbucketType checks if the repository type is for Bitbucket
func (rt RepositoryType) IsBitbucketType() bool {
	return rt == RepositoryTypeBitbucketUser || rt == RepositoryTypeBitbucketWorkspace
}

// String returns the string representation of repository type
func (rt RepositoryType) String() string {
	return string(rt)
}

// RepositoryFilter represents filtering options for repositories
type RepositoryFilter struct {
	IncludeForks bool
	MinSize      int64
	MaxSize      int64
	Languages    []string
	UpdatedAfter time.Time
	OnlyPublic   bool
}

// NewRepositoryFilter creates a new repository filter with defaults
func NewRepositoryFilter() *RepositoryFilter {
	return &RepositoryFilter{
		IncludeForks: false,
		MinSize:      0,
		MaxSize:      -1, // -1 means no limit
		Languages:    []string{},
		OnlyPublic:   true,
	}
}

// ShouldInclude checks if a repository should be included based on the filter
func (rf *RepositoryFilter) ShouldInclude(repo *Repository) bool {
	// Check fork filter
	if !rf.IncludeForks && repo.IsFork {
		return false
	}

	// Check size constraints
	if repo.Size < rf.MinSize {
		return false
	}
	if rf.MaxSize >= 0 && repo.Size > rf.MaxSize {
		return false
	}

	// Check language filter
	if len(rf.Languages) > 0 {
		languageMatch := false
		for _, lang := range rf.Languages {
			if repo.Language == lang {
				languageMatch = true
				break
			}
		}
		if !languageMatch {
			return false
		}
	}

	// Check update time
	if !rf.UpdatedAfter.IsZero() && repo.UpdatedAt.Before(rf.UpdatedAfter) {
		return false
	}

	// Check public/private
	if rf.OnlyPublic && !repo.IsPublic() {
		return false
	}

	return true
}

// PaginationOptions represents pagination settings
type PaginationOptions struct {
	Page    int
	PerPage int
}

// NewPaginationOptions creates pagination options with defaults
func NewPaginationOptions() *PaginationOptions {
	return &PaginationOptions{
		Page:    1,
		PerPage: 100, // GitHub's max per page
	}
}

// Validate ensures pagination options are valid
func (po *PaginationOptions) Validate() error {
	if po.Page < 1 {
		po.Page = 1
	}
	if po.PerPage < 1 || po.PerPage > 100 {
		po.PerPage = 100
	}
	return nil
}

// GetOffset calculates the offset for pagination
func (po *PaginationOptions) GetOffset() int {
	return (po.Page - 1) * po.PerPage
}
