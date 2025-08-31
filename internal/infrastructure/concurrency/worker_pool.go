package concurrency

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"

	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/shared"
	"github.com/italoag/ghcloner/internal/infrastructure/git"
)

// WorkerPool manages concurrent cloning operations using ants
type WorkerPool struct {
	pool            *ants.Pool
	gitClient       *git.GitClient
	logger          shared.Logger
	progressTracker *cloning.ProgressTracker
	results         chan *cloning.JobResult
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	maxRetries      int
	retryDelay      time.Duration
}

// WorkerPoolConfig holds configuration for the worker pool
type WorkerPoolConfig struct {
	MaxWorkers      int
	MaxRetries      int
	RetryDelay      time.Duration
	GitClient       *git.GitClient
	Logger          shared.Logger
	ProgressTracker *cloning.ProgressTracker
}

// NewWorkerPool creates a new worker pool for cloning operations
func NewWorkerPool(config *WorkerPoolConfig) (*WorkerPool, error) {
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU() * 2 // Default to 2x CPU cores
	}

	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay <= 0 {
		config.RetryDelay = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create ants pool with panic handler
	pool, err := ants.NewPool(config.MaxWorkers, ants.WithOptions(ants.Options{
		ExpiryDuration: 10 * time.Second, // Worker expiry time
		PreAlloc:       true,             // Pre-allocate workers
		PanicHandler: func(i interface{}) {
			config.Logger.Error("Worker panic",
				shared.StringField("panic", fmt.Sprintf("%v", i)))
		},
	}))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create worker pool: %w", err)
	}

	wp := &WorkerPool{
		pool:            pool,
		gitClient:       config.GitClient,
		logger:          config.Logger,
		progressTracker: config.ProgressTracker,
		results:         make(chan *cloning.JobResult, config.MaxWorkers*2),
		ctx:             ctx,
		cancel:          cancel,
		maxRetries:      config.MaxRetries,
		retryDelay:      config.RetryDelay,
	}

	config.Logger.Info("Worker pool created",
		shared.IntField("max_workers", config.MaxWorkers),
		shared.IntField("max_retries", config.MaxRetries))

	return wp, nil
}

// SubmitJob submits a cloning job to the worker pool
func (wp *WorkerPool) SubmitJob(job *cloning.CloneJob) error {
	if wp.pool.IsClosed() {
		return fmt.Errorf("worker pool is closed")
	}

	wp.wg.Add(1)

	return wp.pool.Submit(func() {
		defer wp.wg.Done()
		wp.executeJob(job)
	})
}

// SubmitJobs submits multiple cloning jobs to the worker pool
func (wp *WorkerPool) SubmitJobs(jobs []*cloning.CloneJob) error {
	for _, job := range jobs {
		if err := wp.SubmitJob(job); err != nil {
			return fmt.Errorf("failed to submit job %s: %w", job.ID, err)
		}
	}
	return nil
}

// executeJob executes a single cloning job with retry logic
func (wp *WorkerPool) executeJob(job *cloning.CloneJob) {
	startTime := time.Now()

	// Mark job as started
	job.MarkStarted()
	if wp.progressTracker != nil {
		wp.progressTracker.StartJob()
	}

	wp.logger.Info("Starting clone job",
		shared.StringField("job_id", job.ID),
		shared.StringField("repo", job.Repository.GetFullName()),
		shared.StringField("destination", job.GetDestinationPath()))

	var lastErr error
	for attempt := 0; attempt <= wp.maxRetries; attempt++ {
		select {
		case <-wp.ctx.Done():
			wp.handleJobCancellation(job)
			return
		default:
		}

		// Execute the clone operation
		err := wp.gitClient.CloneRepository(wp.ctx, job)

		if err == nil {
			// Success
			wp.handleJobSuccess(job, startTime)
			return
		}

		lastErr = err

		// Check if error is retryable
		if gitValidator := git.NewGitValidator(wp.logger); gitValidator.IsPermanentError(err) {
			// Permanent error, don't retry
			wp.logger.Error("Permanent error, not retrying",
				shared.StringField("job_id", job.ID),
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.ErrorField(err))
			break
		}

		// Check if we should skip (repository already exists)
		if _, ok := err.(*git.RepositoryExistsError); ok {
			wp.handleJobSkipped(job, err.Error())
			return
		}

		// Retry logic
		if attempt < wp.maxRetries {
			wp.logger.Warn("Clone attempt failed, retrying",
				shared.StringField("job_id", job.ID),
				shared.StringField("repo", job.Repository.GetFullName()),
				shared.IntField("attempt", attempt+1),
				shared.IntField("max_attempts", wp.maxRetries+1),
				shared.ErrorField(err))

			// Wait before retry with exponential backoff
			retryDelay := wp.retryDelay * time.Duration(1<<attempt)
			select {
			case <-time.After(retryDelay):
			case <-wp.ctx.Done():
				wp.handleJobCancellation(job)
				return
			}
		}
	}

	// All retries exhausted
	wp.handleJobFailure(job, lastErr)
}

// handleJobSuccess handles successful job completion
func (wp *WorkerPool) handleJobSuccess(job *cloning.CloneJob, startTime time.Time) {
	duration := time.Since(startTime)
	job.MarkCompleted()

	// Calculate repository size
	var repoSize int64
	if size, err := wp.gitClient.GetRepositorySize(job.GetDestinationPath()); err == nil {
		repoSize = size
	}

	// Update progress with detailed information
	if wp.progressTracker != nil {
		wp.progressTracker.CompleteJobWithDetails(
			job.Repository.GetFullName(),
			duration,
			repoSize,
		)
	}

	result := cloning.NewJobResult(job, true, repoSize)

	wp.logger.Info("Clone job completed successfully",
		shared.StringField("job_id", job.ID),
		shared.StringField("repo", job.Repository.GetFullName()),
		shared.DurationField("duration", duration),
		shared.IntField("size_bytes", int(repoSize)))

	select {
	case wp.results <- result:
	case <-wp.ctx.Done():
	}
}

// handleJobFailure handles job failure after all retries
func (wp *WorkerPool) handleJobFailure(job *cloning.CloneJob, err error) {
	duration := job.Duration()
	job.MarkFailed(err)

	// Update progress with detailed information
	if wp.progressTracker != nil {
		wp.progressTracker.FailJobWithDetails(
			job.Repository.GetFullName(),
			duration,
			err,
		)
	}

	result := cloning.NewJobResult(job, false, 0)

	wp.logger.Error("Clone job failed permanently",
		shared.StringField("job_id", job.ID),
		shared.StringField("repo", job.Repository.GetFullName()),
		shared.ErrorField(err))

	select {
	case wp.results <- result:
	case <-wp.ctx.Done():
	}
}

// handleJobSkipped handles skipped jobs (e.g., repository already exists)
func (wp *WorkerPool) handleJobSkipped(job *cloning.CloneJob, reason string) {
	duration := job.Duration()
	job.MarkSkipped(reason)

	// Update progress with detailed information
	if wp.progressTracker != nil {
		wp.progressTracker.SkipJobWithDetails(
			job.Repository.GetFullName(),
			duration,
			reason,
		)
	}

	result := cloning.NewJobResult(job, true, 0) // Consider skipped as success

	wp.logger.Info("Clone job skipped",
		shared.StringField("job_id", job.ID),
		shared.StringField("repo", job.Repository.GetFullName()),
		shared.StringField("reason", reason))

	select {
	case wp.results <- result:
	case <-wp.ctx.Done():
	}
}

// handleJobCancellation handles job cancellation
func (wp *WorkerPool) handleJobCancellation(job *cloning.CloneJob) {
	job.MarkFailed(fmt.Errorf("job cancelled"))

	if wp.progressTracker != nil {
		wp.progressTracker.FailJob()
	}

	wp.logger.Info("Clone job cancelled",
		shared.StringField("job_id", job.ID),
		shared.StringField("repo", job.Repository.GetFullName()))
}

// Wait waits for all submitted jobs to complete
func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
	close(wp.results)
}

// Results returns a channel for receiving job results
func (wp *WorkerPool) Results() <-chan *cloning.JobResult {
	return wp.results
}

// GetProgress returns current progress information
func (wp *WorkerPool) GetProgress() *cloning.Progress {
	if wp.progressTracker != nil {
		return wp.progressTracker.GetProgress()
	}
	return nil
}

// SetProgressTracker sets the progress tracker for this worker pool
func (wp *WorkerPool) SetProgressTracker(tracker *cloning.ProgressTracker) {
	wp.progressTracker = tracker
}

// GetStats returns worker pool statistics
func (wp *WorkerPool) GetStats() *WorkerPoolStats {
	return &WorkerPoolStats{
		TotalWorkers:   wp.pool.Cap(),
		RunningWorkers: wp.pool.Running(),
		FreeWorkers:    wp.pool.Free(),
		SubmittedTasks: 0, // ants v2 doesn't expose this metric
	}
}

// Close gracefully shuts down the worker pool
func (wp *WorkerPool) Close() error {
	wp.logger.Info("Shutting down worker pool")

	// Cancel context to stop new work
	wp.cancel()

	// Wait for current jobs to complete with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		wp.logger.Info("All jobs completed, closing worker pool")
	case <-time.After(30 * time.Second):
		wp.logger.Warn("Timeout waiting for jobs to complete, force closing")
	}

	// Close the ants pool
	wp.pool.Release()

	return nil
}

// ForceClose immediately shuts down the worker pool
func (wp *WorkerPool) ForceClose() error {
	wp.logger.Warn("Force closing worker pool")
	wp.cancel()
	wp.pool.Release()
	return nil
}

// WorkerPoolStats contains statistics about the worker pool
type WorkerPoolStats struct {
	TotalWorkers   int    `json:"total_workers"`
	RunningWorkers int    `json:"running_workers"`
	FreeWorkers    int    `json:"free_workers"`
	SubmittedTasks uint64 `json:"submitted_tasks"`
}

// String returns a string representation of the stats
func (s *WorkerPoolStats) String() string {
	return fmt.Sprintf("Workers: %d/%d running, %d free, %d tasks submitted",
		s.RunningWorkers, s.TotalWorkers, s.FreeWorkers, s.SubmittedTasks)
}

// JobManager manages job prioritization and scheduling
type JobManager struct {
	highPriorityJobs chan *cloning.CloneJob
	normalJobs       chan *cloning.CloneJob
	workerPool       *WorkerPool
	logger           shared.Logger
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

// NewJobManager creates a new job manager
func NewJobManager(workerPool *WorkerPool, logger shared.Logger) *JobManager {
	ctx, cancel := context.WithCancel(context.Background())

	jm := &JobManager{
		highPriorityJobs: make(chan *cloning.CloneJob, 100),
		normalJobs:       make(chan *cloning.CloneJob, 1000),
		workerPool:       workerPool,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
	}

	// Start job scheduler
	jm.wg.Add(1)
	go jm.scheduleJobs()

	return jm
}

// SubmitHighPriorityJob submits a high priority job
func (jm *JobManager) SubmitHighPriorityJob(job *cloning.CloneJob) error {
	select {
	case jm.highPriorityJobs <- job:
		return nil
	case <-jm.ctx.Done():
		return fmt.Errorf("job manager is closed")
	default:
		return fmt.Errorf("high priority job queue is full")
	}
}

// SubmitJob submits a normal priority job
func (jm *JobManager) SubmitJob(job *cloning.CloneJob) error {
	select {
	case jm.normalJobs <- job:
		return nil
	case <-jm.ctx.Done():
		return fmt.Errorf("job manager is closed")
	default:
		return fmt.Errorf("job queue is full")
	}
}

// scheduleJobs handles job scheduling prioritization
func (jm *JobManager) scheduleJobs() {
	defer jm.wg.Done()

	for {
		select {
		case <-jm.ctx.Done():
			return
		case job := <-jm.highPriorityJobs:
			if err := jm.workerPool.SubmitJob(job); err != nil {
				jm.logger.Error("Failed to submit high priority job",
					shared.StringField("job_id", job.ID),
					shared.ErrorField(err))
			}
		case job := <-jm.normalJobs:
			// Check if high priority jobs are waiting
			select {
			case highPriorityJob := <-jm.highPriorityJobs:
				// Submit high priority job first
				if err := jm.workerPool.SubmitJob(highPriorityJob); err != nil {
					jm.logger.Error("Failed to submit high priority job",
						shared.StringField("job_id", highPriorityJob.ID),
						shared.ErrorField(err))
				}
				// Put normal job back in queue
				select {
				case jm.normalJobs <- job:
				default:
					jm.logger.Warn("Normal job queue full, dropping job",
						shared.StringField("job_id", job.ID))
				}
			default:
				// No high priority jobs, submit normal job
				if err := jm.workerPool.SubmitJob(job); err != nil {
					jm.logger.Error("Failed to submit job",
						shared.StringField("job_id", job.ID),
						shared.ErrorField(err))
				}
			}
		}
	}
}

// Close closes the job manager
func (jm *JobManager) Close() error {
	jm.cancel()
	jm.wg.Wait()
	close(jm.highPriorityJobs)
	close(jm.normalJobs)
	return nil
}
