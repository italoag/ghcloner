package bitbucket

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/infrastructure/logging"
)

func TestNewBitbucketClient(t *testing.T) {
	logger, err := logging.NewZapLogger(&logging.LoggerConfig{
		Level:       "info",
		Encoding:    "console",
		Development: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	config := &BitbucketClientConfig{
		Username:    "testuser",
		AppPassword: "testpass",
		Logger:      logger,
	}

	client := NewBitbucketClient(config)

	assert.NotNil(t, client)
	assert.Equal(t, "https://api.bitbucket.org/2.0", client.baseURL)
	assert.Equal(t, "ghclone/1.0", client.userAgent)
	assert.Equal(t, "testuser", client.username)
	assert.Equal(t, "testpass", client.appPassword)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

func TestBitbucketClient_ConvertToDomainRepository(t *testing.T) {
	logger, err := logging.NewZapLogger(&logging.LoggerConfig{
		Level:       "info",
		Encoding:    "console",
		Development: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	client := NewBitbucketClient(&BitbucketClientConfig{
		Logger: logger,
	})

	apiRepo := &BitbucketAPIResponse{
		UUID:     "{12345678-1234-1234-1234-123456789abc}",
		Name:     "test-repo",
		FullName: "testuser/test-repo",
		Size:     1024,
		Owner: OwnerInfo{
			Username: "testuser",
		},
		Links: LinksInfo{
			Clone: []CloneLink{
				{Name: "https", Href: "https://bitbucket.org/testuser/test-repo.git"},
				{Name: "ssh", Href: "git@bitbucket.org:testuser/test-repo.git"},
			},
		},
		MainBranch: &MainBranch{Name: "main"},
		Parent:     nil, // Not a fork
	}

	repo, err := client.convertToDomainRepository(apiRepo)

	require.NoError(t, err)
	assert.Equal(t, "test-repo", repo.Name)
	assert.Equal(t, "https://bitbucket.org/testuser/test-repo.git", repo.CloneURL)
	assert.Equal(t, "testuser", repo.Owner)
	assert.Equal(t, int64(1024), repo.Size)
	assert.Equal(t, "main", repo.DefaultBranch)
	assert.False(t, repo.IsFork)
}

func TestBitbucketClient_ConvertForkRepository(t *testing.T) {
	logger, err := logging.NewZapLogger(&logging.LoggerConfig{
		Level:       "info",
		Encoding:    "console",
		Development: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	client := NewBitbucketClient(&BitbucketClientConfig{
		Logger: logger,
	})

	apiRepo := &BitbucketAPIResponse{
		UUID:     "{12345678-1234-1234-1234-123456789abc}",
		Name:     "forked-repo",
		FullName: "testuser/forked-repo",
		Size:     2048,
		Owner: OwnerInfo{
			Username: "testuser",
		},
		Links: LinksInfo{
			Clone: []CloneLink{
				{Name: "https", Href: "https://bitbucket.org/testuser/forked-repo.git"},
			},
		},
		MainBranch: &MainBranch{Name: "develop"},
		Parent: &ParentRepo{ // This indicates it's a fork
			UUID:     "{parent-uuid}",
			FullName: "originaluser/original-repo",
		},
	}

	repo, err := client.convertToDomainRepository(apiRepo)

	require.NoError(t, err)
	assert.Equal(t, "forked-repo", repo.Name)
	assert.Equal(t, "https://bitbucket.org/testuser/forked-repo.git", repo.CloneURL)
	assert.Equal(t, "testuser", repo.Owner)
	assert.Equal(t, int64(2048), repo.Size)
	assert.Equal(t, "develop", repo.DefaultBranch)
	assert.True(t, repo.IsFork) // This should be detected as a fork
}

func TestRepositoryType_IsBitbucketType(t *testing.T) {
	tests := []struct {
		repoType repository.RepositoryType
		expected bool
	}{
		{repository.RepositoryTypeBitbucketUser, true},
		{repository.RepositoryTypeBitbucketWorkspace, true},
		{repository.RepositoryTypeUser, false},
		{repository.RepositoryTypeOrganization, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.repoType), func(t *testing.T) {
			result := tt.repoType.IsBitbucketType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRepositoryType_IsGitHubType(t *testing.T) {
	tests := []struct {
		repoType repository.RepositoryType
		expected bool
	}{
		{repository.RepositoryTypeUser, true},
		{repository.RepositoryTypeOrganization, true},
		{repository.RepositoryTypeBitbucketUser, false},
		{repository.RepositoryTypeBitbucketWorkspace, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.repoType), func(t *testing.T) {
			result := tt.repoType.IsGitHubType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRepositoryType_IsValid(t *testing.T) {
	tests := []struct {
		repoType repository.RepositoryType
		expected bool
	}{
		{repository.RepositoryTypeUser, true},
		{repository.RepositoryTypeOrganization, true},
		{repository.RepositoryTypeBitbucketUser, true},
		{repository.RepositoryTypeBitbucketWorkspace, true},
		{repository.RepositoryType("invalid"), false},
		{repository.RepositoryType(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.repoType), func(t *testing.T) {
			result := tt.repoType.IsValid()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Integration test (requires actual credentials - skipped by default)
func TestBitbucketClient_FetchRepositories_Integration(t *testing.T) {
	t.Skip("Integration test - requires real Bitbucket credentials")

	// This test would require real Bitbucket credentials
	username := "your-bitbucket-username"
	appPassword := "your-app-password"
	
	if username == "your-bitbucket-username" {
		t.Skip("Please set real credentials to run integration test")
	}

	logger, err := logging.NewZapLogger(&logging.LoggerConfig{
		Level:       "info",
		Encoding:    "console",
		Development: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	client := NewBitbucketClient(&BitbucketClientConfig{
		Username:    username,
		AppPassword: appPassword,
		Logger:      logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test fetching user repositories
	repos, err := client.FetchRepositories(
		ctx,
		username,
		repository.RepositoryTypeBitbucketUser,
		repository.NewRepositoryFilter(),
		&repository.PaginationOptions{Page: 1, PerPage: 10},
	)

	require.NoError(t, err)
	assert.NotNil(t, repos)
	// Additional assertions would depend on the actual repositories
}