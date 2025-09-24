package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/italoag/repocloner/internal/domain/cloning"
	"github.com/italoag/repocloner/internal/domain/repository"
	"github.com/italoag/repocloner/internal/domain/shared"
	"github.com/italoag/repocloner/internal/infrastructure/concurrency"
	"github.com/italoag/repocloner/internal/infrastructure/git"
)

// CloningService provides high-level cloning operations
type CloningService struct {
	workerPool      *concurrency.WorkerPool
	jobManager      *concurrency.JobManager
	gitClient       *git.GitClient
	domainService   *cloning.DomainCloneService
	logger          shared.Logger
	progressTracker *cloning.ProgressTracker
	mu              sync.RWMutex
	activeJobs      map[string]*cloning.CloneJob
}

// CloningServiceConfig holds configuration for cloning service
type CloningServiceConfig struct {
	WorkerPool    *concurrency.WorkerPool
	GitClient     *git.GitClient
	Logger        shared.Logger
	MaxConcurrent int
}

// NewCloningService creates a new cloning service
func NewCloningService(config *CloningServiceConfig) (*CloningService, error) {
	if config.WorkerPool == nil {
		return nil, fmt.Errorf("worker pool is required")
	}
	if config.GitClient == nil {
		return nil, fmt.Errorf("git client is required")
	}
	if config.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	domainService := cloning.NewDomainCloneService(config.Logger.With(shared.StringField("component", "domain_service")))
	jobManager := concurrency.NewJobManager(config.WorkerPool, config.Logger)

	return &CloningService{
		workerPool:    config.WorkerPool,
		jobManager:    jobManager,
		gitClient:     config.GitClient,
		domainService: domainService,
		logger:        config.Logger.With(shared.StringField("service", "cloning")),
		activeJobs:    make(map[string]*cloning.CloneJob),
	}, nil
}

// CloneBatchRequest represents a request to clone multiple repositories
type CloneBatchRequest struct {
	Repositories  []*repository.Repository
	BaseDirectory string
	Options       *cloning.CloneOptions
	BatchID       string
}

// CloneBatchResponse represents the response from batch cloning
type CloneBatchResponse struct {
	BatchID       string
	TotalJobs     int
	SubmittedJobs int
	FailedJobs    int
	Progress      *cloning.Progress
}

// CloneBatch clones multiple repositories concurrently
func (s *CloningService) CloneBatch(
	ctx context.Context,
	req *CloneBatchRequest,
) (*CloneBatchResponse, error) {
	if err := s.validateBatchRequest(req); err != nil {
		return nil, fmt.Errorf("invalid batch request: %w", err)
	}

	// Set defaults
	if req.Options == nil {
		req.Options = cloning.NewDefaultCloneOptions()
	}
	if req.BatchID == "" {
		req.BatchID = fmt.Sprintf("batch_%d", time.Now().UnixNano())
	}

	s.logger.Info("Starting batch clone operation",
		shared.StringField("batch_id", req.BatchID),
		shared.IntField("repository_count", len(req.Repositories)),
		shared.StringField("base_directory", req.BaseDirectory))

	// Create progress tracker for this batch
	progressTracker := cloning.NewProgressTracker(len(req.Repositories))
	s.mu.Lock()
	s.progressTracker = progressTracker
	s.mu.Unlock()

	// Create and validate jobs
	jobs := s.createJobsFromRepositories(req.Repositories, req.BaseDirectory, req.Options)
	validJobs := s.filterValidJobs(jobs)

	s.logger.Info("Jobs created and validated",
		shared.StringField("batch_id", req.BatchID),
		shared.IntField("total_jobs", len(jobs)),
		shared.IntField("valid_jobs", len(validJobs)))

	// Submit jobs to worker pool
	submittedJobs := 0
	failedJobs := 0

	for _, job := range validJobs {
		s.mu.Lock()
		s.activeJobs[job.ID] = job
		s.mu.Unlock()

		// Determine job priority
		priority := s.domainService.CalculateJobPriority(job)

		var err error
		if priority > 5 {
			err = s.jobManager.SubmitHighPriorityJob(job)
		} else {
			err = s.jobManager.SubmitJob(job)
		}

		if err != nil {
			s.logger.Error("Failed to submit job",
				shared.StringField("job_id", job.ID),
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.ErrorField(err))
			failedJobs++
		} else {
			submittedJobs++
		}
	}

	s.logger.Info("Batch jobs submitted",
		shared.StringField("batch_id", req.BatchID),
		shared.IntField("submitted", submittedJobs),
		shared.IntField("failed", failedJobs))

	return &CloneBatchResponse{
		BatchID:       req.BatchID,
		TotalJobs:     len(jobs),
		SubmittedJobs: submittedJobs,
		FailedJobs:    failedJobs,
		Progress:      progressTracker.GetProgress(),
	}, nil
}

// CloneSingleRequest represents a request to clone a single repository
type CloneSingleRequest struct {
	Repository    *repository.Repository
	BaseDirectory string
	Options       *cloning.CloneOptions
	Priority      int
}

// CloneSingleResponse represents the response from single repository cloning
type CloneSingleResponse struct {
	JobID    string
	Status   cloning.JobStatus
	Duration time.Duration
	Error    error
}

// CloneSingle clones a single repository
func (s *CloningService) CloneSingle(
	ctx context.Context,
	req *CloneSingleRequest,
) (*CloneSingleResponse, error) {
	if err := s.validateSingleRequest(req); err != nil {
		return nil, fmt.Errorf("invalid single clone request: %w", err)
	}

	// Set defaults
	if req.Options == nil {
		req.Options = cloning.NewDefaultCloneOptions()
	}

	s.logger.Info("Starting single clone operation",
		shared.StringField("repo", req.Repository.GetFullName()),
		shared.StringField("destination", req.BaseDirectory))

	// Create job
	job := cloning.NewCloneJob(req.Repository, req.BaseDirectory, req.Options)

	// Validate job
	if err := s.domainService.IsJobExecutable(job); err != nil {
		return nil, fmt.Errorf("job not executable: %w", err)
	}

	startTime := time.Now()

	// Track active job
	s.mu.Lock()
	s.activeJobs[job.ID] = job
	s.mu.Unlock()

	// Submit job based on priority
	var err error
	if req.Priority > 5 {
		err = s.jobManager.SubmitHighPriorityJob(job)
	} else {
		err = s.jobManager.SubmitJob(job)
	}

	if err != nil {
		s.mu.Lock()
		delete(s.activeJobs, job.ID)
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to submit job: %w", err)
	}

	// Wait for completion or timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute) // 30 minute timeout for single clone

	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			currentJob, exists := s.activeJobs[job.ID]
			s.mu.RUnlock()

			if !exists || currentJob.Status == cloning.JobStatusCompleted ||
				currentJob.Status == cloning.JobStatusFailed ||
				currentJob.Status == cloning.JobStatusSkipped {

				duration := time.Since(startTime)

				s.mu.Lock()
				delete(s.activeJobs, job.ID)
				s.mu.Unlock()

				var jobError error
				if currentJob != nil {
					jobError = currentJob.Error
				}

				return &CloneSingleResponse{
					JobID:    job.ID,
					Status:   currentJob.Status,
					Duration: duration,
					Error:    jobError,
				}, nil
			}

		case <-ctx.Done():
			s.mu.Lock()
			delete(s.activeJobs, job.ID)
			s.mu.Unlock()
			return nil, ctx.Err()

		case <-timeout:
			s.mu.Lock()
			delete(s.activeJobs, job.ID)
			s.mu.Unlock()
			return nil, fmt.Errorf("clone operation timed out")
		}
	}
}

// GetProgress returns the current progress of cloning operations
func (s *CloningService) GetProgress() *cloning.Progress {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.progressTracker != nil {
		return s.progressTracker.GetProgress()
	}
	return nil
}

// GetActiveJobs returns currently active jobs
func (s *CloningService) GetActiveJobs() []*cloning.CloneJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*cloning.CloneJob, 0, len(s.activeJobs))
	for _, job := range s.activeJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// CancelJob cancels a specific job
func (s *CloningService) CancelJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.activeJobs[jobID]
	if !exists {
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.Status == cloning.JobStatusCompleted ||
		job.Status == cloning.JobStatusFailed ||
		job.Status == cloning.JobStatusSkipped {
		return fmt.Errorf("job %s is already finished", jobID)
	}

	job.MarkFailed(fmt.Errorf("cancelled by user"))
	delete(s.activeJobs, jobID)

	s.logger.Info("Job cancelled",
		shared.StringField("job_id", jobID),
		shared.StringField("repo", job.Repository.GetFullName()))

	return nil
}

// CancelAllJobs cancels all active jobs
func (s *CloningService) CancelAllJobs() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cancelledCount := 0
	for jobID, job := range s.activeJobs {
		if job.Status == cloning.JobStatusRunning || job.Status == cloning.JobStatusPending {
			job.MarkFailed(fmt.Errorf("cancelled by user"))
			delete(s.activeJobs, jobID)
			cancelledCount++
		}
	}

	s.logger.Info("All jobs cancelled",
		shared.IntField("cancelled_count", cancelledCount))

	return nil
}

// Close gracefully shuts down the cloning service
func (s *CloningService) Close() error {
	s.logger.Info("Shutting down cloning service")

	// Cancel all active jobs
	if err := s.CancelAllJobs(); err != nil {
		s.logger.Error("Failed to cancel all jobs", shared.ErrorField(err))
	}

	// Close job manager
	if err := s.jobManager.Close(); err != nil {
		s.logger.Error("Failed to close job manager", shared.ErrorField(err))
		return err
	}

	// Close progress tracker
	s.mu.Lock()
	if s.progressTracker != nil {
		s.progressTracker.Close()
		s.progressTracker = nil
	}
	s.mu.Unlock()

	return nil
}

// Helper methods

func (s *CloningService) createJobsFromRepositories(
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

func (s *CloningService) filterValidJobs(jobs []*cloning.CloneJob) []*cloning.CloneJob {
	var validJobs []*cloning.CloneJob

	for _, job := range jobs {
		if err := s.domainService.IsJobExecutable(job); err != nil {
			s.logger.Warn("Filtering out invalid job",
				shared.StringField("job_id", job.ID),
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.ErrorField(err))
			continue
		}
		validJobs = append(validJobs, job)
	}

	return validJobs
}

func (s *CloningService) validateBatchRequest(req *CloneBatchRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if len(req.Repositories) == 0 {
		return fmt.Errorf("repositories list cannot be empty")
	}
	if req.BaseDirectory == "" {
		return fmt.Errorf("base directory cannot be empty")
	}

	// Validate base directory
	if err := s.domainService.ValidateDestination(req.BaseDirectory); err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
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

	return nil
}

func (s *CloningService) validateSingleRequest(req *CloneSingleRequest) error {
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

	// Validate base directory
	if err := s.domainService.ValidateDestination(req.BaseDirectory); err != nil {
		return fmt.Errorf("invalid base directory: %w", err)
	}

	return nil
}
