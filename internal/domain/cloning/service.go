package cloning

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/italoag/ghcloner/internal/domain/repository"
	"github.com/italoag/ghcloner/internal/domain/shared"
)

// Cloning domain errors
var (
	ErrInvalidDestination     = errors.New("invalid destination path")
	ErrDestinationNotWritable = errors.New("destination path is not writable")
	ErrJobAlreadyRunning      = errors.New("job is already running")
	ErrJobNotFound            = errors.New("job not found")
	ErrCloneTimeout           = errors.New("clone operation timed out")
	ErrInvalidCloneOptions    = errors.New("invalid clone options")
)

// DomainCloneService implements core cloning business logic
type DomainCloneService struct {
	logger shared.Logger
}

// NewDomainCloneService creates a new domain clone service
func NewDomainCloneService(logger shared.Logger) *DomainCloneService {
	return &DomainCloneService{logger: logger}
}

// ValidateJob validates a clone job before execution
func (s *DomainCloneService) ValidateJob(job *CloneJob) error {
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}

	if job.Repository == nil {
		return fmt.Errorf("job must have a repository")
	}

	if err := job.Repository.Validate(); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}

	if job.BaseDirectory == "" {
		return fmt.Errorf("base directory cannot be empty")
	}

	if job.Options == nil {
		return fmt.Errorf("clone options cannot be nil")
	}

	if err := job.Options.Validate(); err != nil {
		return fmt.Errorf("invalid clone options: %w", err)
	}

	return nil
}

// ValidateDestination checks if a destination path is valid for cloning
func (s *DomainCloneService) ValidateDestination(path string) error {
	if path == "" {
		return ErrInvalidDestination
	}

	// Check if path is absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: path must be absolute", ErrInvalidDestination)
	}

	// Check parent directory exists, create if it doesn't
	parentDir := filepath.Dir(path)
	if info, err := os.Stat(parentDir); err != nil {
		if os.IsNotExist(err) {
			// Create parent directory
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("%w: failed to create parent directory: %v", ErrInvalidDestination, err)
			}
		} else {
			return fmt.Errorf("%w: cannot access parent directory: %v", ErrInvalidDestination, err)
		}
	} else if !info.IsDir() {
		return fmt.Errorf("%w: parent path is not a directory", ErrInvalidDestination)
	}

	// Test write permissions by trying to create a temporary file
	tempFile := filepath.Join(parentDir, ".ghclone_test")
	if file, err := os.Create(tempFile); err != nil {
		return fmt.Errorf("%w: %v", ErrDestinationNotWritable, err)
	} else {
		if err := file.Close(); err != nil {
			s.logger.Warn("failed to close temporary file", shared.ErrorField(err))
		}
		if err := os.Remove(tempFile); err != nil {
			s.logger.Warn("failed to remove temporary file", shared.ErrorField(err))
		}
	}

	return nil
}

// PrepareDestination creates necessary directories for cloning
func (s *DomainCloneService) PrepareDestination(path string) error {
	if err := s.ValidateDestination(filepath.Dir(path)); err != nil {
		return err
	}

	// Create the directory structure
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory structure: %w", err)
	}

	return nil
}

// ShouldSkipRepository determines if a repository should be skipped
func (s *DomainCloneService) ShouldSkipRepository(
	repo *repository.Repository,
	filter *repository.RepositoryFilter,
) bool {
	if filter == nil {
		return false
	}

	return !filter.ShouldInclude(repo)
}

// CalculateJobPriority calculates the priority of a clone job
func (s *DomainCloneService) CalculateJobPriority(job *CloneJob) int {
	priority := 0

	// Higher priority for smaller repositories (faster to clone)
	if job.Repository.Size < 1024*1024 { // < 1MB
		priority += 10
	} else if job.Repository.Size < 10*1024*1024 { // < 10MB
		priority += 5
	}

	// Higher priority for non-fork repositories
	if !job.Repository.IsFork {
		priority += 3
	}

	// Lower priority for retried jobs
	priority -= job.RetryCount * 2

	return priority
}

// EstimateCloneDuration estimates how long a clone operation might take
func (s *DomainCloneService) EstimateCloneDuration(repo *repository.Repository) int64 {
	// Base time in seconds
	baseTime := int64(5)

	// Add time based on repository size (rough estimate)
	sizeBasedTime := repo.Size / (1024 * 1024) // 1 second per MB

	return baseTime + sizeBasedTime
}

// IsJobExecutable checks if a job can be executed
func (s *DomainCloneService) IsJobExecutable(job *CloneJob) error {
	if job.Status == JobStatusRunning {
		return ErrJobAlreadyRunning
	}

	if job.Status == JobStatusCompleted {
		return fmt.Errorf("job already completed")
	}

	if job.Status == JobStatusFailed && !job.CanRetry() {
		return fmt.Errorf("job failed and cannot be retried")
	}

	return s.ValidateJob(job)
}
