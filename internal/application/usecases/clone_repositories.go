package usecases

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/italoag/repocloner/internal/domain/cloning"
	"github.com/italoag/repocloner/internal/domain/repository"
	"github.com/italoag/repocloner/internal/domain/shared"
	"github.com/italoag/repocloner/internal/infrastructure/concurrency"
)

// CloneRepositoriesRequest represents the input for cloning repositories
type CloneRepositoriesRequest struct {
	Repositories  []*repository.Repository
	BaseDirectory string
	Options       *cloning.CloneOptions
	Concurrency   int
}

// CloneRepositoriesResponse represents the output of cloning repositories
type CloneRepositoriesResponse struct {
	TotalJobs     int
	CompletedJobs int
	FailedJobs    int
	SkippedJobs   int
	TotalDuration time.Duration
	Results       []*cloning.JobResult
	Progress      *cloning.Progress
}

// CloneRepositoriesUseCase handles the business logic for cloning multiple repositories
type CloneRepositoriesUseCase struct {
	workerPool      *concurrency.WorkerPool
	domainService   *cloning.DomainCloneService
	logger          shared.Logger
	progressTracker *cloning.ProgressTracker
}

// NewCloneRepositoriesUseCase creates a new clone repositories use case
func NewCloneRepositoriesUseCase(
	workerPool *concurrency.WorkerPool,
	domainService *cloning.DomainCloneService,
	logger shared.Logger,
) *CloneRepositoriesUseCase {
	return &CloneRepositoriesUseCase{
		workerPool:    workerPool,
		domainService: domainService,
		logger:        logger,
	}
}

// Execute executes the clone repositories use case
func (uc *CloneRepositoriesUseCase) Execute(
	ctx context.Context,
	req *CloneRepositoriesRequest,
) (*CloneRepositoriesResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Set defaults
	if req.Options == nil {
		req.Options = cloning.NewDefaultCloneOptions()
	}

	startTime := time.Now()

	uc.logger.Info("Starting concurrent repository cloning",
		shared.IntField("repository_count", len(req.Repositories)),
		shared.StringField("base_directory", req.BaseDirectory),
		shared.IntField("concurrency", req.Concurrency))

	// Create jobs
	jobs := uc.createCloneJobs(req.Repositories, req.BaseDirectory, req.Options)

	// Filter jobs based on domain rules
	validJobs := uc.filterValidJobs(jobs)

	uc.logger.Info("Jobs created and filtered",
		shared.IntField("total_jobs", len(jobs)),
		shared.IntField("valid_jobs", len(validJobs)))

	// Create progress tracker with valid job count
	progressTracker := cloning.NewProgressTracker(len(validJobs))
	uc.progressTracker = progressTracker

	// Set progress tracker on worker pool for real-time updates
	uc.workerPool.SetProgressTracker(progressTracker)

	// Submit jobs to worker pool
	if err := uc.workerPool.SubmitJobs(validJobs); err != nil {
		return nil, fmt.Errorf("failed to submit jobs: %w", err)
	}

	// Collect results
	results := uc.collectResults(ctx, len(validJobs))

	// Wait for all jobs to complete
	uc.workerPool.Wait()

	// Ensure final progress update shows 100% completion
	finalProgress := progressTracker.GetProgress()

	// Log progress state for debugging
	uc.logger.Info("Progress state after worker pool completion",
		shared.IntField("total", finalProgress.Total),
		shared.IntField("completed", finalProgress.Completed),
		shared.IntField("failed", finalProgress.Failed),
		shared.IntField("skipped", finalProgress.Skipped),
		shared.IntField("in_progress", finalProgress.InProgress))

	// Force completion if somehow not detected
	if !finalProgress.IsComplete() {
		uc.logger.Warn("Forcing completion state - jobs finished but progress incomplete",
			shared.IntField("completed", finalProgress.Completed),
			shared.IntField("failed", finalProgress.Failed),
			shared.IntField("skipped", finalProgress.Skipped),
			shared.IntField("total", finalProgress.Total),
			shared.IntField("in_progress", finalProgress.InProgress))

		// First try to synchronize progress properly
		progressTracker.ForceSynchronize()
		finalProgress = progressTracker.GetProgress()

		// If still not complete, force remaining jobs
		for finalProgress.InProgress > 0 {
			progressTracker.CompleteJob() // Mark remaining as completed instead of failed
			finalProgress = progressTracker.GetProgress()
			uc.logger.Debug("Forced completion of remaining job",
				shared.IntField("remaining_in_progress", finalProgress.InProgress))
		}

		// Update final progress after forced completion
		finalProgress = progressTracker.GetProgress()
		uc.logger.Info("Final progress after forced completion",
			shared.IntField("completed", finalProgress.Completed),
			shared.IntField("failed", finalProgress.Failed),
			shared.IntField("skipped", finalProgress.Skipped),
			shared.IntField("in_progress", finalProgress.InProgress))
	}

	// Give a moment for the final progress update to propagate to TUI
	time.Sleep(200 * time.Millisecond)

	// Clear progress tracker from worker pool to avoid state leaking
	uc.workerPool.SetProgressTracker(nil)
	uc.progressTracker = nil

	totalDuration := time.Since(startTime)
	finalProgress = progressTracker.GetProgress() // Get final progress after cleanup

	uc.logger.Info("Repository cloning completed",
		shared.IntField("total_jobs", len(validJobs)),
		shared.IntField("completed", finalProgress.Completed),
		shared.IntField("failed", finalProgress.Failed),
		shared.IntField("skipped", finalProgress.Skipped),
		shared.DurationField("total_duration", totalDuration))

	return &CloneRepositoriesResponse{
		TotalJobs:     len(validJobs),
		CompletedJobs: finalProgress.Completed,
		FailedJobs:    finalProgress.Failed,
		SkippedJobs:   finalProgress.Skipped,
		TotalDuration: totalDuration,
		Results:       results,
		Progress:      finalProgress,
	}, nil
}

// createCloneJobs creates clone jobs from repositories
func (uc *CloneRepositoriesUseCase) createCloneJobs(
	repos []*repository.Repository,
	baseDir string,
	options *cloning.CloneOptions,
) []*cloning.CloneJob {
	jobs := make([]*cloning.CloneJob, len(repos))
	for i, repo := range repos {
		jobs[i] = cloning.NewCloneJob(repo, baseDir, options)
	}
	return jobs
}

// filterValidJobs filters jobs based on domain rules
func (uc *CloneRepositoriesUseCase) filterValidJobs(jobs []*cloning.CloneJob) []*cloning.CloneJob {
	var validJobs []*cloning.CloneJob

	for _, job := range jobs {
		if err := uc.domainService.IsJobExecutable(job); err != nil {
			uc.logger.Warn("Job filtered out",
				shared.StringField("job_id", job.ID),
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.ErrorField(err))
			continue
		}

		validJobs = append(validJobs, job)
	}

	return validJobs
}

// collectResults collects results from worker pool
func (uc *CloneRepositoriesUseCase) collectResults(ctx context.Context, expectedResults int) []*cloning.JobResult {
	var results []*cloning.JobResult
	var mu sync.Mutex

	resultsChan := uc.workerPool.Results()

	// Collect results
	for i := 0; i < expectedResults; i++ {
		select {
		case result := <-resultsChan:
			if result != nil {
				mu.Lock()
				results = append(results, result)
				mu.Unlock()

				uc.logger.Debug("Job result collected",
					shared.StringField("job_id", result.Job.ID),
					shared.StringField("repo", result.Job.Repository.GetFullName()),
					shared.StringField("status", result.Job.Status.String()),
					shared.DurationField("duration", result.Duration))
			}
		case <-ctx.Done():
			uc.logger.Warn("Context cancelled while collecting results")
			return results
		}
	}

	return results
}

// validateRequest validates the clone repositories request
func (uc *CloneRepositoriesUseCase) validateRequest(req *CloneRepositoriesRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	if len(req.Repositories) == 0 {
		return fmt.Errorf("repositories list cannot be empty")
	}

	if req.BaseDirectory == "" {
		return fmt.Errorf("base directory cannot be empty")
	}

	// Validate base directory using domain service
	if err := uc.domainService.ValidateDestination(req.BaseDirectory); err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
	}

	if req.Concurrency < 0 {
		return fmt.Errorf("concurrency cannot be negative")
	}

	// Validate repositories
	for i, repo := range req.Repositories {
		if repo == nil {
			return fmt.Errorf("repository at index %d is nil", i)
		}
		if err := repo.Validate(); err != nil {
			return fmt.Errorf("invalid repository at index %d: %w", i, err)
		}
	}

	// Validate clone options if provided
	if req.Options != nil {
		if err := req.Options.Validate(); err != nil {
			return fmt.Errorf("invalid clone options: %w", err)
		}
	}

	return nil
}

// GetProgress returns the current progress
func (uc *CloneRepositoriesUseCase) GetProgress() *cloning.Progress {
	if uc.progressTracker != nil {
		return uc.progressTracker.GetProgress()
	}
	return nil
}

// EstimateDuration estimates how long the cloning operation will take
func (uc *CloneRepositoriesUseCase) EstimateDuration(repositories []*repository.Repository) time.Duration {
	if len(repositories) == 0 {
		return 0
	}

	var totalEstimate int64
	for _, repo := range repositories {
		estimate := uc.domainService.EstimateCloneDuration(repo)
		totalEstimate += estimate
	}

	// With concurrency, divide by number of workers (roughly)
	stats := uc.workerPool.GetStats()
	if stats.TotalWorkers > 0 {
		totalEstimate = totalEstimate / int64(stats.TotalWorkers)
	}

	return time.Duration(totalEstimate) * time.Second
}

// CloneSingleRepositoryRequest represents input for cloning a single repository
type CloneSingleRepositoryRequest struct {
	Repository    *repository.Repository
	BaseDirectory string
	Options       *cloning.CloneOptions
}

// CloneSingleRepositoryResponse represents output for cloning a single repository
type CloneSingleRepositoryResponse struct {
	Job      *cloning.CloneJob
	Result   *cloning.JobResult
	Duration time.Duration
}

// CloneSingleRepositoryUseCase handles cloning a single repository
type CloneSingleRepositoryUseCase struct {
	workerPool    *concurrency.WorkerPool
	domainService *cloning.DomainCloneService
	logger        shared.Logger
}

// NewCloneSingleRepositoryUseCase creates a new clone single repository use case
func NewCloneSingleRepositoryUseCase(
	workerPool *concurrency.WorkerPool,
	domainService *cloning.DomainCloneService,
	logger shared.Logger,
) *CloneSingleRepositoryUseCase {
	return &CloneSingleRepositoryUseCase{
		workerPool:    workerPool,
		domainService: domainService,
		logger:        logger,
	}
}

// Execute executes the clone single repository use case
func (uc *CloneSingleRepositoryUseCase) Execute(
	ctx context.Context,
	req *CloneSingleRepositoryRequest,
) (*CloneSingleRepositoryResponse, error) {
	// Validate request
	if err := uc.validateSingleRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Set defaults
	if req.Options == nil {
		req.Options = cloning.NewDefaultCloneOptions()
	}

	startTime := time.Now()

	uc.logger.Info("Starting single repository clone",
		shared.StringField("repo", req.Repository.GetFullName()),
		shared.StringField("destination", req.BaseDirectory))

	// Create job
	job := cloning.NewCloneJob(req.Repository, req.BaseDirectory, req.Options)

	// Validate job
	if err := uc.domainService.IsJobExecutable(job); err != nil {
		return nil, fmt.Errorf("job not executable: %w", err)
	}

	// Submit job
	if err := uc.workerPool.SubmitJob(job); err != nil {
		return nil, fmt.Errorf("failed to submit job: %w", err)
	}

	// Wait for result
	resultsChan := uc.workerPool.Results()
	select {
	case result := <-resultsChan:
		duration := time.Since(startTime)

		uc.logger.Info("Single repository clone completed",
			shared.StringField("repo", req.Repository.GetFullName()),
			shared.StringField("status", job.Status.String()),
			shared.DurationField("duration", duration))

		return &CloneSingleRepositoryResponse{
			Job:      job,
			Result:   result,
			Duration: duration,
		}, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// validateSingleRequest validates a single repository clone request
func (uc *CloneSingleRepositoryUseCase) validateSingleRequest(req *CloneSingleRepositoryRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	if req.Repository == nil {
		return fmt.Errorf("repository cannot be nil")
	}

	if err := req.Repository.Validate(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	if req.BaseDirectory == "" {
		return fmt.Errorf("base directory cannot be empty")
	}

	if err := uc.domainService.ValidateDestination(req.BaseDirectory); err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
	}

	if req.Options != nil {
		if err := req.Options.Validate(); err != nil {
			return fmt.Errorf("invalid clone options: %w", err)
		}
	}

	return nil
}
