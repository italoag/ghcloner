package cloning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/italoag/ghcloner/internal/domain/repository"
)

func TestNewCloneJob(t *testing.T) {
	repo := createTestRepository()
	baseDir := "/tmp/test"
	options := NewDefaultCloneOptions()

	job := NewCloneJob(repo, baseDir, options)

	assert.NotEmpty(t, job.ID)
	assert.Equal(t, repo, job.Repository)
	assert.Equal(t, baseDir, job.BaseDirectory)
	assert.Equal(t, options, job.Options)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.Equal(t, 3, job.MaxRetries)
}

func TestCloneJob_GetDestinationPath(t *testing.T) {
	repo := createTestRepository()
	
	tests := []struct {
		name      string
		baseDir   string
		createOrg bool
		expected  string
	}{
		{
			name:      "simple path",
			baseDir:   "/tmp",
			createOrg: false,
			expected:  "/tmp/test-repo",
		},
		{
			name:      "org directory structure",
			baseDir:   "/tmp",
			createOrg: true,
			expected:  "/tmp/test-owner/test-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewDefaultCloneOptions()
			options.CreateOrgDirs = tt.createOrg
			
			job := NewCloneJob(repo, tt.baseDir, options)
			assert.Equal(t, tt.expected, job.GetDestinationPath())
		})
	}
}

func TestCloneJob_CanRetry(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())

	// Initially can't retry (status is pending)
	assert.False(t, job.CanRetry())

	// After marking as failed
	job.MarkFailed(assert.AnError)
	assert.True(t, job.CanRetry())

	// After max retries
	job.RetryCount = job.MaxRetries
	assert.False(t, job.CanRetry())

	// Completed jobs can't retry
	job.RetryCount = 0
	job.MarkCompleted()
	assert.False(t, job.CanRetry())
}

func TestCloneJob_MarkStarted(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	
	startTime := time.Now()
	job.MarkStarted()
	
	assert.Equal(t, JobStatusRunning, job.Status)
	assert.WithinDuration(t, startTime, job.StartedAt, time.Second)
}

func TestCloneJob_MarkCompleted(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	job.MarkStarted()
	
	completedTime := time.Now()
	job.MarkCompleted()
	
	assert.Equal(t, JobStatusCompleted, job.Status)
	assert.WithinDuration(t, completedTime, job.CompletedAt, time.Second)
	assert.NoError(t, job.Error)
}

func TestCloneJob_MarkFailed(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	job.MarkStarted()
	
	testError := assert.AnError
	failedTime := time.Now()
	job.MarkFailed(testError)
	
	assert.Equal(t, JobStatusFailed, job.Status)
	assert.WithinDuration(t, failedTime, job.CompletedAt, time.Second)
	assert.Equal(t, testError, job.Error)
}

func TestCloneJob_MarkSkipped(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	
	reason := "repository already exists"
	skippedTime := time.Now()
	job.MarkSkipped(reason)
	
	assert.Equal(t, JobStatusSkipped, job.Status)
	assert.WithinDuration(t, skippedTime, job.CompletedAt, time.Second)
	assert.Contains(t, job.Error.Error(), reason)
}

func TestCloneJob_Retry(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	job.MarkFailed(assert.AnError)
	
	initialRetryCount := job.RetryCount
	job.Retry()
	
	assert.Equal(t, initialRetryCount+1, job.RetryCount)
	assert.Equal(t, JobStatusPending, job.Status)
	assert.NoError(t, job.Error)
}

func TestCloneJob_Duration(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	
	// Before starting
	assert.Equal(t, time.Duration(0), job.Duration())
	
	// After starting
	job.MarkStarted()
	time.Sleep(10 * time.Millisecond)
	duration := job.Duration()
	assert.True(t, duration > 0)
	
	// After completion
	job.MarkCompleted()
	finalDuration := job.Duration()
	assert.True(t, finalDuration >= duration)
}

func TestCloneJob_ShouldSkipExisting(t *testing.T) {
	// This test would require file system operations
	// For now, we'll test the basic logic
	options := NewDefaultCloneOptions()
	options.SkipExisting = true
	
	job := NewCloneJob(createTestRepository(), "/tmp", options)
	
	// Without actual directory, should return false
	assert.False(t, job.ShouldSkipExisting())
}

func TestCloneOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		options *CloneOptions
		wantErr bool
	}{
		{
			name:    "valid options",
			options: NewDefaultCloneOptions(),
			wantErr: false,
		},
		{
			name: "negative depth",
			options: &CloneOptions{
				Depth: -1,
			},
			wantErr: true,
		},
		{
			name: "zero depth is valid",
			options: &CloneOptions{
				Depth: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.options.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJobStatus_String(t *testing.T) {
	tests := []struct {
		status   JobStatus
		expected string
	}{
		{JobStatusPending, "pending"},
		{JobStatusRunning, "running"},
		{JobStatusCompleted, "completed"},
		{JobStatusFailed, "failed"},
		{JobStatusSkipped, "skipped"},
		{JobStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestNewJobResult(t *testing.T) {
	job := NewCloneJob(createTestRepository(), "/tmp", NewDefaultCloneOptions())
	job.MarkStarted()
	time.Sleep(10 * time.Millisecond)
	job.MarkCompleted()
	
	success := true
	bytesSize := int64(1024)
	
	result := NewJobResult(job, success, bytesSize)
	
	assert.Equal(t, job, result.Job)
	assert.Equal(t, success, result.Success)
	assert.Equal(t, bytesSize, result.BytesSize)
	assert.True(t, result.Duration > 0)
}

// Helper function to create a test repository
func createTestRepository() *repository.Repository {
	repo, _ := repository.NewRepository(
		12345,
		"test-repo",
		"https://github.com/test-owner/test-repo.git",
		"test-owner",
		false,
		1024,
		"main",
	)
	return repo
}