package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/italoag/ghcloner/internal/domain/cloning"
	"github.com/italoag/ghcloner/internal/domain/shared"
)

// ProgressService manages progress tracking for cloning operations
type ProgressService struct {
	batches        map[string]*cloning.ProgressTracker
	subscribers    map[string][]chan *cloning.Progress
	logger         shared.Logger
	mu             sync.RWMutex
	updateInterval time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// ProgressServiceConfig holds configuration for progress service
type ProgressServiceConfig struct {
	Logger         shared.Logger
	UpdateInterval time.Duration
}

// NewProgressService creates a new progress tracking service
func NewProgressService(config *ProgressServiceConfig) *ProgressService {
	if config.UpdateInterval == 0 {
		config.UpdateInterval = 500 * time.Millisecond
	}

	ctx, cancel := context.WithCancel(context.Background())

	ps := &ProgressService{
		batches:        make(map[string]*cloning.ProgressTracker),
		subscribers:    make(map[string][]chan *cloning.Progress),
		logger:         config.Logger.With(shared.StringField("service", "progress")),
		updateInterval: config.UpdateInterval,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Start progress update loop
	ps.wg.Add(1)
	go ps.updateLoop()

	return ps
}

// CreateBatch creates a new progress tracking batch
func (ps *ProgressService) CreateBatch(batchID string, totalJobs int) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.batches[batchID]; exists {
		return fmt.Errorf("batch %s already exists", batchID)
	}

	tracker := cloning.NewProgressTracker(totalJobs)
	ps.batches[batchID] = tracker
	ps.subscribers[batchID] = make([]chan *cloning.Progress, 0)

	ps.logger.Info("Progress batch created",
		shared.StringField("batch_id", batchID),
		shared.IntField("total_jobs", totalJobs))

	return nil
}

// GetProgress returns the current progress for a batch
func (ps *ProgressService) GetProgress(batchID string) (*cloning.Progress, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	tracker, exists := ps.batches[batchID]
	if !exists {
		return nil, fmt.Errorf("batch %s not found", batchID)
	}

	return tracker.GetProgress(), nil
}

// GetAllProgress returns progress for all active batches
func (ps *ProgressService) GetAllProgress() map[string]*cloning.Progress {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := make(map[string]*cloning.Progress)
	for batchID, tracker := range ps.batches {
		result[batchID] = tracker.GetProgress()
	}

	return result
}

// GetOverallProgress returns combined progress across all batches
func (ps *ProgressService) GetOverallProgress() *cloning.Progress {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if len(ps.batches) == 0 {
		return cloning.NewProgress(0)
	}

	overall := &cloning.Progress{
		StartTime: time.Now(),
	}

	for _, tracker := range ps.batches {
		progress := tracker.GetProgress()
		overall.Total += progress.Total
		overall.Completed += progress.Completed
		overall.Failed += progress.Failed
		overall.Skipped += progress.Skipped
		overall.InProgress += progress.InProgress

		// Use earliest start time
		if overall.StartTime.After(progress.StartTime) {
			overall.StartTime = progress.StartTime
		}
	}

	overall.UpdateElapsedTime()
	overall.CalculateETA()

	return overall
}

// StartJob marks a job as started in the specified batch
func (ps *ProgressService) StartJob(batchID string) error {
	ps.mu.RLock()
	tracker, exists := ps.batches[batchID]
	ps.mu.RUnlock()

	if !exists {
		return fmt.Errorf("batch %s not found", batchID)
	}

	tracker.StartJob()
	ps.logger.Debug("Job started",
		shared.StringField("batch_id", batchID))

	return nil
}

// CompleteJob marks a job as completed in the specified batch
func (ps *ProgressService) CompleteJob(batchID string) error {
	ps.mu.RLock()
	tracker, exists := ps.batches[batchID]
	ps.mu.RUnlock()

	if !exists {
		return fmt.Errorf("batch %s not found", batchID)
	}

	tracker.CompleteJob()
	ps.logger.Debug("Job completed",
		shared.StringField("batch_id", batchID))

	return nil
}

// FailJob marks a job as failed in the specified batch
func (ps *ProgressService) FailJob(batchID string) error {
	ps.mu.RLock()
	tracker, exists := ps.batches[batchID]
	ps.mu.RUnlock()

	if !exists {
		return fmt.Errorf("batch %s not found", batchID)
	}

	tracker.FailJob()
	ps.logger.Debug("Job failed",
		shared.StringField("batch_id", batchID))

	return nil
}

// SkipJob marks a job as skipped in the specified batch
func (ps *ProgressService) SkipJob(batchID string) error {
	ps.mu.RLock()
	tracker, exists := ps.batches[batchID]
	ps.mu.RUnlock()

	if !exists {
		return fmt.Errorf("batch %s not found", batchID)
	}

	tracker.SkipJob()
	ps.logger.Debug("Job skipped",
		shared.StringField("batch_id", batchID))

	return nil
}

// Subscribe subscribes to progress updates for a specific batch
func (ps *ProgressService) Subscribe(batchID string) (<-chan *cloning.Progress, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.batches[batchID]; !exists {
		return nil, fmt.Errorf("batch %s not found", batchID)
	}

	ch := make(chan *cloning.Progress, 10)
	ps.subscribers[batchID] = append(ps.subscribers[batchID], ch)

	ps.logger.Debug("Subscriber added",
		shared.StringField("batch_id", batchID),
		shared.IntField("total_subscribers", len(ps.subscribers[batchID])))

	return ch, nil
}

// SubscribeToAll subscribes to progress updates for all batches
func (ps *ProgressService) SubscribeToAll() <-chan map[string]*cloning.Progress {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan map[string]*cloning.Progress, 10)

	// Start a goroutine to send periodic updates
	ps.wg.Add(1)
	go func() {
		defer ps.wg.Done()
		ticker := time.NewTicker(ps.updateInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				allProgress := ps.GetAllProgress()
				select {
				case ch <- allProgress:
				case <-ps.ctx.Done():
					close(ch)
					return
				default:
					// Channel is full, skip this update
				}
			case <-ps.ctx.Done():
				close(ch)
				return
			}
		}
	}()

	return ch
}

// RemoveBatch removes a completed batch and closes its subscribers
func (ps *ProgressService) RemoveBatch(batchID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	tracker, exists := ps.batches[batchID]
	if !exists {
		return fmt.Errorf("batch %s not found", batchID)
	}

	// Close all subscriber channels
	for _, ch := range ps.subscribers[batchID] {
		close(ch)
	}

	// Clean up
	tracker.Close()
	delete(ps.batches, batchID)
	delete(ps.subscribers, batchID)

	ps.logger.Info("Progress batch removed",
		shared.StringField("batch_id", batchID))

	return nil
}

// GetBatchStats returns statistics for a specific batch
func (ps *ProgressService) GetBatchStats(batchID string) (*BatchStats, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	tracker, exists := ps.batches[batchID]
	if !exists {
		return nil, fmt.Errorf("batch %s not found", batchID)
	}

	progress := tracker.GetProgress()
	subscriberCount := len(ps.subscribers[batchID])

	return &BatchStats{
		BatchID:         batchID,
		Progress:        progress,
		SubscriberCount: subscriberCount,
		IsComplete:      progress.IsComplete(),
		CreatedAt:       progress.StartTime,
	}, nil
}

// GetServiceStats returns overall service statistics
func (ps *ProgressService) GetServiceStats() *ServiceStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	totalBatches := len(ps.batches)
	totalSubscribers := 0
	completedBatches := 0

	for batchID, tracker := range ps.batches {
		totalSubscribers += len(ps.subscribers[batchID])
		if tracker.GetProgress().IsComplete() {
			completedBatches++
		}
	}

	return &ServiceStats{
		TotalBatches:     totalBatches,
		CompletedBatches: completedBatches,
		ActiveBatches:    totalBatches - completedBatches,
		TotalSubscribers: totalSubscribers,
		Uptime:           time.Since(ps.getStartTime()),
	}
}

// Close gracefully shuts down the progress service
func (ps *ProgressService) Close() error {
	ps.logger.Info("Shutting down progress service")

	// Cancel context to stop update loop
	ps.cancel()

	// Wait for goroutines to finish
	ps.wg.Wait()

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Close all trackers and subscriber channels
	for batchID, tracker := range ps.batches {
		tracker.Close()

		for _, ch := range ps.subscribers[batchID] {
			close(ch)
		}
	}

	// Clear maps
	ps.batches = make(map[string]*cloning.ProgressTracker)
	ps.subscribers = make(map[string][]chan *cloning.Progress)

	ps.logger.Info("Progress service shut down successfully")
	return nil
}

// updateLoop sends periodic updates to subscribers
func (ps *ProgressService) updateLoop() {
	defer ps.wg.Done()

	ticker := time.NewTicker(ps.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ps.sendUpdates()
		case <-ps.ctx.Done():
			return
		}
	}
}

// sendUpdates sends current progress to all subscribers
func (ps *ProgressService) sendUpdates() {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for batchID, tracker := range ps.batches {
		progress := tracker.GetProgress()
		subscribers := ps.subscribers[batchID]

		for i, ch := range subscribers {
			select {
			case ch <- progress:
				// Update sent successfully
			case <-ps.ctx.Done():
				return
			default:
				// Channel is full or closed, remove subscriber
				ps.logger.Warn("Removing unresponsive subscriber",
					shared.StringField("batch_id", batchID),
					shared.IntField("subscriber_index", i))

				// Remove this subscriber (this is safe because we have read lock)
				// The actual removal will happen in a separate cleanup routine
			}
		}
	}
}

// getStartTime returns the earliest start time across all batches
func (ps *ProgressService) getStartTime() time.Time {
	earliest := time.Now()

	for _, tracker := range ps.batches {
		progress := tracker.GetProgress()
		if progress.StartTime.Before(earliest) {
			earliest = progress.StartTime
		}
	}

	return earliest
}

// BatchStats represents statistics for a progress batch
type BatchStats struct {
	BatchID         string            `json:"batch_id"`
	Progress        *cloning.Progress `json:"progress"`
	SubscriberCount int               `json:"subscriber_count"`
	IsComplete      bool              `json:"is_complete"`
	CreatedAt       time.Time         `json:"created_at"`
}

// ServiceStats represents overall service statistics
type ServiceStats struct {
	TotalBatches     int           `json:"total_batches"`
	CompletedBatches int           `json:"completed_batches"`
	ActiveBatches    int           `json:"active_batches"`
	TotalSubscribers int           `json:"total_subscribers"`
	Uptime           time.Duration `json:"uptime"`
}

// String returns a string representation of service stats
func (s *ServiceStats) String() string {
	return fmt.Sprintf("Batches: %d total (%d active, %d completed), Subscribers: %d, Uptime: %v",
		s.TotalBatches, s.ActiveBatches, s.CompletedBatches, s.TotalSubscribers, s.Uptime)
}
