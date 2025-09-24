package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/italoag/repocloner/internal/application/services"
	"github.com/italoag/repocloner/internal/domain/repository"
	"github.com/italoag/repocloner/internal/infrastructure/concurrency"
	"github.com/italoag/repocloner/internal/infrastructure/git"
	"github.com/italoag/repocloner/internal/infrastructure/github"
	"github.com/italoag/repocloner/internal/infrastructure/logging"
)

// Integration test configuration
type TestConfig struct {
	GitHubToken     string
	TestOwner       string
	TestRepoType    repository.RepositoryType
	MaxWorkers      int
	TestTimeout     time.Duration
	SkipGitTests    bool
	SkipGitHubTests bool
}

func getTestConfig() *TestConfig {
	return &TestConfig{
		GitHubToken:     "", // Set via environment or skip GitHub tests
		TestOwner:       "octocat",
		TestRepoType:    repository.RepositoryTypeUser,
		MaxWorkers:      2,
		TestTimeout:     30 * time.Second,
		SkipGitTests:    false,
		SkipGitHubTests: false, // Skip by default to avoid rate limiting
	}
}

func TestGitHubClient_Integration(t *testing.T) {
	config := getTestConfig()
	if config.SkipGitHubTests {
		t.Skip("Skipping GitHub integration tests")
	}

	logger := logging.NewNoOpLogger()

	client := github.NewGitHubClient(&github.GitHubClientConfig{
		Token:       config.GitHubToken,
		UserAgent:   "repocloner-test/1.0",
		Timeout:     config.TestTimeout,
		RateLimiter: github.NewNoOpRateLimiter(),
		Logger:      logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), config.TestTimeout)
	defer cancel()

	filter := repository.NewRepositoryFilter()
	filter.IncludeForks = false

	repos, err := client.FetchRepositories(ctx, config.TestOwner, config.TestRepoType, filter, nil)

	if config.GitHubToken == "" {
		// Without token, we might get rate limited, but shouldn't crash
		if err != nil {
			t.Logf("Expected error without token: %v", err)
		}
		return
	}

	require.NoError(t, err)
	assert.NotEmpty(t, repos)

	// Validate repository structure
	for _, repo := range repos {
		assert.NoError(t, repo.Validate())
		assert.Equal(t, config.TestOwner, repo.Owner)
		assert.False(t, repo.IsFork) // We filtered out forks
	}
}

func TestGitClient_Integration(t *testing.T) {
	config := getTestConfig()
	if config.SkipGitTests {
		t.Skip("Skipping Git integration tests")
	}

	logger := logging.NewNoOpLogger()

	gitClient, err := git.NewGitClient(&git.GitClientConfig{
		Timeout: config.TestTimeout,
		Logger:  logger,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), config.TestTimeout)
	defer cancel()

	// Test Git installation validation
	err = gitClient.ValidateGitInstallation(ctx)
	assert.NoError(t, err, "Git should be installed for integration tests")
}

func TestWorkerPool_Integration(t *testing.T) {
	logger := logging.NewNoOpLogger()

	gitClient, err := git.NewGitClient(&git.GitClientConfig{
		Timeout: 30 * time.Second,
		Logger:  logger,
	})
	require.NoError(t, err)

	workerPool, err := concurrency.NewWorkerPool(&concurrency.WorkerPoolConfig{
		MaxWorkers: 2,
		MaxRetries: 1,
		RetryDelay: 1 * time.Second,
		GitClient:  gitClient,
		Logger:     logger,
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, workerPool.Close())
	}()

	// Test worker pool stats
	stats := workerPool.GetStats()
	assert.Equal(t, 2, stats.TotalWorkers)
	assert.Equal(t, 2, stats.FreeWorkers)
	assert.Equal(t, 0, stats.RunningWorkers)
}

func TestProgressService_Integration(t *testing.T) {
	logger := logging.NewNoOpLogger()

	progressService := services.NewProgressService(&services.ProgressServiceConfig{
		Logger:         logger,
		UpdateInterval: 100 * time.Millisecond,
	})
	defer func() {
		require.NoError(t, progressService.Close())
	}()

	batchID := "test-batch"
	totalJobs := 5

	// Create a batch
	err := progressService.CreateBatch(batchID, totalJobs)
	require.NoError(t, err)

	// Get initial progress
	progress, err := progressService.GetProgress(batchID)
	require.NoError(t, err)
	assert.Equal(t, totalJobs, progress.Total)
	assert.Equal(t, 0, progress.Completed)

	// Simulate job progression
	err = progressService.StartJob(batchID)
	require.NoError(t, err)

	err = progressService.CompleteJob(batchID)
	require.NoError(t, err)

	// Check progress
	progress, err = progressService.GetProgress(batchID)
	require.NoError(t, err)
	assert.Equal(t, 1, progress.Completed)
	assert.Equal(t, 0, progress.InProgress)

	// Clean up
	err = progressService.RemoveBatch(batchID)
	require.NoError(t, err)
}
