package cloning

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/italoag/ghcloner/internal/domain/repository"
)

// JobStatus represents the status of a clone job
type JobStatus int

const (
	JobStatusPending JobStatus = iota
	JobStatusRunning
	JobStatusCompleted
	JobStatusFailed
	JobStatusSkipped
)

// String returns the string representation of job status
func (js JobStatus) String() string {
	switch js {
	case JobStatusPending:
		return "pending"
	case JobStatusRunning:
		return "running"
	case JobStatusCompleted:
		return "completed"
	case JobStatusFailed:
		return "failed"
	case JobStatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// CloneOptions represents options for cloning repositories
type CloneOptions struct {
	Depth             int
	RecurseSubmodules bool
	Branch            string
	SkipExisting      bool
	CreateOrgDirs     bool
}

// NewDefaultCloneOptions creates clone options with sensible defaults
func NewDefaultCloneOptions() *CloneOptions {
	return &CloneOptions{
		Depth:             1, // Shallow clone by default
		RecurseSubmodules: true,
		Branch:            "", // Use default branch
		SkipExisting:      true,
		CreateOrgDirs:     false,
	}
}

// Validate ensures clone options are valid
func (co *CloneOptions) Validate() error {
	if co.Depth < 0 {
		return fmt.Errorf("depth cannot be negative")
	}
	return nil
}

// CloneJob represents a repository cloning job
type CloneJob struct {
	ID            string
	Repository    *repository.Repository
	BaseDirectory string
	Options       *CloneOptions
	Status        JobStatus
	StartedAt     time.Time
	CompletedAt   time.Time
	Error         error
	RetryCount    int
	MaxRetries    int
}

// NewCloneJob creates a new clone job
func NewCloneJob(
	repo *repository.Repository,
	baseDir string,
	options *CloneOptions,
) *CloneJob {
	if options == nil {
		options = NewDefaultCloneOptions()
	}

	return &CloneJob{
		ID:            generateJobID(),
		Repository:    repo,
		BaseDirectory: baseDir,
		Options:       options,
		Status:        JobStatusPending,
		MaxRetries:    3,
	}
}

// GetDestinationPath returns the full path where the repository will be cloned
func (cj *CloneJob) GetDestinationPath() string {
	if cj.Options.CreateOrgDirs {
		return fmt.Sprintf("%s/%s/%s", cj.BaseDirectory, cj.Repository.Owner, cj.Repository.Name)
	}
	return cj.Repository.GetLocalPath(cj.BaseDirectory)
}

// CanRetry checks if the job can be retried
func (cj *CloneJob) CanRetry() bool {
	return cj.RetryCount < cj.MaxRetries && cj.Status == JobStatusFailed
}

// MarkStarted marks the job as started
func (cj *CloneJob) MarkStarted() {
	cj.Status = JobStatusRunning
	cj.StartedAt = time.Now()
}

// MarkCompleted marks the job as successfully completed
func (cj *CloneJob) MarkCompleted() {
	cj.Status = JobStatusCompleted
	cj.CompletedAt = time.Now()
	cj.Error = nil
}

// MarkFailed marks the job as failed with an error
func (cj *CloneJob) MarkFailed(err error) {
	cj.Status = JobStatusFailed
	cj.CompletedAt = time.Now()
	cj.Error = err
}

// MarkSkipped marks the job as skipped
func (cj *CloneJob) MarkSkipped(reason string) {
	cj.Status = JobStatusSkipped
	cj.CompletedAt = time.Now()
	cj.Error = fmt.Errorf("skipped: %s", reason)
}

// Retry increments retry count and resets status
func (cj *CloneJob) Retry() {
	if cj.CanRetry() {
		cj.RetryCount++
		cj.Status = JobStatusPending
		cj.Error = nil
	}
}

// Duration returns the duration of the job execution
func (cj *CloneJob) Duration() time.Duration {
	if cj.StartedAt.IsZero() {
		return 0
	}
	if cj.CompletedAt.IsZero() {
		return time.Since(cj.StartedAt)
	}
	return cj.CompletedAt.Sub(cj.StartedAt)
}

// ShouldSkipExisting checks if existing directories should be skipped
func (cj *CloneJob) ShouldSkipExisting() bool {
	if !cj.Options.SkipExisting {
		return false
	}

	destPath := cj.GetDestinationPath()
	if _, err := os.Stat(destPath); err == nil {
		return true // Directory exists
	}
	return false
}

// CloneService defines the interface for cloning operations
type CloneService interface {
	// CloneRepository clones a single repository
	CloneRepository(ctx context.Context, job *CloneJob) error

	// ValidateDestination checks if the destination is valid for cloning
	ValidateDestination(path string) error

	// PrepareDestination creates necessary directories
	PrepareDestination(path string) error
}

// JobResult represents the result of a clone job
type JobResult struct {
	Job       *CloneJob
	Duration  time.Duration
	BytesSize int64
	Success   bool
}

// NewJobResult creates a new job result
func NewJobResult(job *CloneJob, success bool, bytesSize int64) *JobResult {
	return &JobResult{
		Job:       job,
		Duration:  job.Duration(),
		BytesSize: bytesSize,
		Success:   success,
	}
}

// generateJobID generates a unique job ID
func generateJobID() string {
	return fmt.Sprintf("job_%d", time.Now().UnixNano())
}