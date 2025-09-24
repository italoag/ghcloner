package usecases

import (
	"context"
	"fmt"

	"github.com/italoag/repocloner/internal/domain/repository"
	"github.com/italoag/repocloner/internal/domain/shared"
	"github.com/italoag/repocloner/internal/infrastructure/bitbucket"
	"github.com/italoag/repocloner/internal/infrastructure/github"
)

// FetchRepositoriesRequest represents the input for fetching repositories
type FetchRepositoriesRequest struct {
	Owner      string
	Type       repository.RepositoryType
	Filter     *repository.RepositoryFilter
	Pagination *repository.PaginationOptions
}

// FetchRepositoriesResponse represents the output of fetching repositories
type FetchRepositoriesResponse struct {
	Repositories []*repository.Repository
	TotalCount   int
	FilteredOut  int
}

// FetchRepositoriesUseCase handles the business logic for fetching repositories
type FetchRepositoriesUseCase struct {
	githubClient    *github.GitHubClient
	bitbucketClient *bitbucket.BitbucketClient
	logger          shared.Logger
}

// NewFetchRepositoriesUseCase creates a new fetch repositories use case
func NewFetchRepositoriesUseCase(
	githubClient *github.GitHubClient,
	bitbucketClient *bitbucket.BitbucketClient,
	logger shared.Logger,
) *FetchRepositoriesUseCase {
	return &FetchRepositoriesUseCase{
		githubClient:    githubClient,
		bitbucketClient: bitbucketClient,
		logger:          logger,
	}
}

// Execute executes the fetch repositories use case
func (uc *FetchRepositoriesUseCase) Execute(
	ctx context.Context,
	req *FetchRepositoriesRequest,
) (*FetchRepositoriesResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Set defaults
	if req.Filter == nil {
		req.Filter = repository.NewRepositoryFilter()
	}
	if req.Pagination == nil {
		req.Pagination = repository.NewPaginationOptions()
	}

	uc.logger.Info("Fetching repositories",
		shared.StringField("owner", req.Owner),
		shared.StringField("type", req.Type.String()),
		shared.IntField("page", req.Pagination.Page),
		shared.IntField("per_page", req.Pagination.PerPage))

	// Fetch repositories from appropriate provider
	var repositories []*repository.Repository
	var err error

	switch {
	case req.Type.IsGitHubType():
		if uc.githubClient == nil {
			return nil, fmt.Errorf("GitHub client not configured")
		}
		repositories, err = uc.githubClient.FetchRepositories(
			ctx,
			req.Owner,
			req.Type,
			req.Filter,
			req.Pagination,
		)
	case req.Type.IsBitbucketType():
		if uc.bitbucketClient == nil {
			return nil, fmt.Errorf("bitbucket client not configured")
		}
		repositories, err = uc.bitbucketClient.FetchRepositories(
			ctx,
			req.Owner,
			req.Type,
			req.Filter,
			req.Pagination,
		)
	default:
		return nil, fmt.Errorf("unsupported repository type: %s", req.Type)
	}

	if err != nil {
		uc.logger.Error("Failed to fetch repositories",
			shared.StringField("owner", req.Owner),
			shared.ErrorField(err))
		return nil, fmt.Errorf("failed to fetch repositories: %w", err)
	}

	totalCount := len(repositories)
	filteredRepositories := make([]*repository.Repository, 0, totalCount)

	// Apply additional filtering if needed
	filteredOut := 0
	for _, repo := range repositories {
		if req.Filter.ShouldInclude(repo) {
			filteredRepositories = append(filteredRepositories, repo)
		} else {
			filteredOut++
		}
	}

	uc.logger.Info("Repositories fetched successfully",
		shared.StringField("owner", req.Owner),
		shared.IntField("total", totalCount),
		shared.IntField("included", len(filteredRepositories)),
		shared.IntField("filtered_out", filteredOut))

	return &FetchRepositoriesResponse{
		Repositories: filteredRepositories,
		TotalCount:   totalCount,
		FilteredOut:  filteredOut,
	}, nil
}

// validateRequest validates the fetch repositories request
func (uc *FetchRepositoriesUseCase) validateRequest(req *FetchRepositoriesRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	if req.Owner == "" {
		return fmt.Errorf("owner cannot be empty")
	}

	if !req.Type.IsValid() {
		return fmt.Errorf("invalid repository type: %s", req.Type)
	}

	if req.Pagination != nil {
		if err := req.Pagination.Validate(); err != nil {
			return fmt.Errorf("invalid pagination: %w", err)
		}
	}

	return nil
}

// ValidateOwnerExistsRequest represents input for validating owner existence
type ValidateOwnerExistsRequest struct {
	Owner string
	Type  repository.RepositoryType
}

// ValidateOwnerExistsUseCase validates if a GitHub user or organization exists
type ValidateOwnerExistsUseCase struct {
	githubClient *github.GitHubClient
	logger       shared.Logger
}

// NewValidateOwnerExistsUseCase creates a new validate owner exists use case
func NewValidateOwnerExistsUseCase(
	githubClient *github.GitHubClient,
	logger shared.Logger,
) *ValidateOwnerExistsUseCase {
	return &ValidateOwnerExistsUseCase{
		githubClient: githubClient,
		logger:       logger,
	}
}

// Execute executes the validate owner exists use case
func (uc *ValidateOwnerExistsUseCase) Execute(
	ctx context.Context,
	req *ValidateOwnerExistsRequest,
) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	if req.Owner == "" {
		return fmt.Errorf("owner cannot be empty")
	}

	if !req.Type.IsValid() {
		return fmt.Errorf("invalid repository type: %s", req.Type)
	}

	uc.logger.Debug("Validating owner existence",
		shared.StringField("owner", req.Owner),
		shared.StringField("type", req.Type.String()))

	// Try to fetch one repository to validate owner exists
	_, err := uc.githubClient.FetchRepositories(
		ctx,
		req.Owner,
		req.Type,
		repository.NewRepositoryFilter(),
		&repository.PaginationOptions{Page: 1, PerPage: 1},
	)

	if err != nil {
		if repository.IsNotFoundError(err) {
			return fmt.Errorf("owner '%s' not found", req.Owner)
		}
		return fmt.Errorf("failed to validate owner: %w", err)
	}

	uc.logger.Debug("Owner exists",
		shared.StringField("owner", req.Owner),
		shared.StringField("type", req.Type.String()))

	return nil
}
