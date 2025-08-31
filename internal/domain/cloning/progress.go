package cloning

import (
	"fmt"
	"sync"
	"time"
)

// RecentCompletion represents a recently completed repository
type RecentCompletion struct {
	Repository  string        `json:"repository"`
	Status      JobStatus     `json:"status"`
	CompletedAt time.Time     `json:"completed_at"`
	Duration    time.Duration `json:"duration"`
	Size        int64         `json:"size"`
	Error       string        `json:"error,omitempty"`
}

// Progress represents the current state of cloning operations
type Progress struct {
	Total            int               `json:"total"`
	Completed        int               `json:"completed"`
	Failed           int               `json:"failed"`
	Skipped          int               `json:"skipped"`
	InProgress       int               `json:"in_progress"`
	ElapsedTime      time.Duration     `json:"elapsed_time"`
	ETA              time.Duration     `json:"eta"`
	StartTime        time.Time         `json:"start_time"`
	Throughput       float64           `json:"throughput"` // Jobs per second
	RecentCompletion *RecentCompletion `json:"recent_completion,omitempty"`
	LastUpdate       time.Time         `json:"last_update"`
}

// NewProgress creates a new progress tracker
func NewProgress(total int) *Progress {
	return &Progress{
		Total:      total,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
	}
}

// UpdateRecentCompletion updates the most recent completion
func (p *Progress) UpdateRecentCompletion(repo string, status JobStatus, duration time.Duration, size int64, err error) {
	errorStr := ""
	if err != nil {
		errorStr = err.Error()
	}

	p.RecentCompletion = &RecentCompletion{
		Repository:  repo,
		Status:      status,
		CompletedAt: time.Now(),
		Duration:    duration,
		Size:        size,
		Error:       errorStr,
	}
	p.LastUpdate = time.Now()
}

// GetPercentage returns the completion percentage
func (p *Progress) GetPercentage() float64 {
	if p.Total == 0 {
		return 100.0
	}
	processed := float64(p.Completed + p.Failed + p.Skipped)
	total := float64(p.Total)
	percentage := (processed / total) * 100.0

	// Ensure we don't exceed 100% due to floating point precision
	if percentage > 100.0 {
		percentage = 100.0
	}

	return percentage
}

// GetSuccessRate returns the success rate as a percentage
func (p *Progress) GetSuccessRate() float64 {
	processed := p.Completed + p.Failed + p.Skipped
	if processed == 0 {
		return 0.0
	}
	return float64(p.Completed) / float64(processed) * 100.0
}

// IsComplete checks if all jobs are finished
func (p *Progress) IsComplete() bool {
	processed := p.Completed + p.Failed + p.Skipped
	// Ensure we handle edge cases where processed might exceed total
	return processed >= p.Total && p.InProgress == 0
}

// UpdateElapsedTime updates the elapsed time
func (p *Progress) UpdateElapsedTime() {
	p.ElapsedTime = time.Since(p.StartTime)
	p.LastUpdate = time.Now()
}

// CalculateETA estimates the time remaining
func (p *Progress) CalculateETA() {
	if p.Total == 0 || p.IsComplete() {
		p.ETA = 0
		return
	}

	p.UpdateElapsedTime()
	processed := p.Completed + p.Failed + p.Skipped

	if processed == 0 {
		p.ETA = 0
		return
	}

	// Calculate throughput
	p.Throughput = float64(processed) / p.ElapsedTime.Seconds()

	if p.Throughput > 0 {
		remaining := p.Total - processed - p.InProgress
		p.ETA = time.Duration(float64(remaining)/p.Throughput) * time.Second
	}
}

// String returns a formatted string representation
func (p *Progress) String() string {
	return ""
}

// ProgressTracker manages progress tracking for clone operations
type ProgressTracker struct {
	progress *Progress
	mutex    sync.RWMutex
	updates  chan *Progress
	done     chan struct{}
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(total int) *ProgressTracker {
	return &ProgressTracker{
		progress: NewProgress(total),
		updates:  make(chan *Progress, 10),
		done:     make(chan struct{}),
	}
}

// GetProgress returns a copy of the current progress
func (pt *ProgressTracker) GetProgress() *Progress {
	pt.mutex.RLock()
	defer pt.mutex.RUnlock()

	// Create a copy to avoid race conditions
	progressCopy := *pt.progress
	progressCopy.CalculateETA()
	return &progressCopy
}

// StartJob marks a job as started
func (pt *ProgressTracker) StartJob() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	pt.progress.InProgress++
	pt.notifyUpdate()
}

// CompleteJob marks a job as completed
func (pt *ProgressTracker) CompleteJob() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Completed++
	pt.notifyUpdate()
}

// CompleteJobWithDetails marks a job as completed with detailed information
func (pt *ProgressTracker) CompleteJobWithDetails(repo string, duration time.Duration, size int64) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Completed++
	pt.progress.UpdateRecentCompletion(repo, JobStatusCompleted, duration, size, nil)
	pt.notifyUpdate()
}

// FailJob marks a job as failed
func (pt *ProgressTracker) FailJob() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Failed++
	pt.notifyUpdate()
}

// FailJobWithDetails marks a job as failed with detailed information
func (pt *ProgressTracker) FailJobWithDetails(repo string, duration time.Duration, err error) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Failed++
	pt.progress.UpdateRecentCompletion(repo, JobStatusFailed, duration, 0, err)
	pt.notifyUpdate()
}

// SkipJob marks a job as skipped
func (pt *ProgressTracker) SkipJob() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Skipped++
	pt.notifyUpdate()
}

// SkipJobWithDetails marks a job as skipped with detailed information
func (pt *ProgressTracker) SkipJobWithDetails(repo string, duration time.Duration, reason string) {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Ensure we don't go negative
	if pt.progress.InProgress > 0 {
		pt.progress.InProgress--
	}
	pt.progress.Skipped++
	pt.progress.UpdateRecentCompletion(repo, JobStatusSkipped, duration, 0, fmt.Errorf("skipped: %s", reason))
	pt.notifyUpdate()
}

// Subscribe returns a channel for progress updates
func (pt *ProgressTracker) Subscribe() <-chan *Progress {
	return pt.updates
}

// ForceSynchronize forces progress to be consistent with expected totals
// This should only be used as a last resort when jobs are complete but progress is inconsistent
func (pt *ProgressTracker) ForceSynchronize() {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	processed := pt.progress.Completed + pt.progress.Failed + pt.progress.Skipped

	// If we have InProgress jobs but all jobs should be done, convert them to completed
	if processed < pt.progress.Total && pt.progress.InProgress > 0 {
		// Move remaining InProgress to Completed to reach total
		remaining := pt.progress.Total - processed
		if remaining <= pt.progress.InProgress {
			pt.progress.Completed += remaining
			pt.progress.InProgress -= remaining
		} else {
			// All InProgress become completed
			pt.progress.Completed += pt.progress.InProgress
			pt.progress.InProgress = 0
		}
	}

	// Ensure InProgress is never negative
	if pt.progress.InProgress < 0 {
		pt.progress.InProgress = 0
	}

	pt.notifyUpdate()
}

// Close stops the progress tracker
func (pt *ProgressTracker) Close() {
	close(pt.done)
	close(pt.updates)
}

// notifyUpdate sends a progress update (must be called with mutex held)
func (pt *ProgressTracker) notifyUpdate() {
	progressCopy := *pt.progress
	progressCopy.CalculateETA()

	// Validate progress consistency
	pt.validateProgressConsistency(&progressCopy)

	select {
	case pt.updates <- &progressCopy:
	case <-pt.done:
	default:
		// Channel is full, skip this update
	}
}

// validateProgressConsistency ensures progress counts are logically consistent
func (pt *ProgressTracker) validateProgressConsistency(progress *Progress) {
	processed := progress.Completed + progress.Failed + progress.Skipped
	totalActive := processed + progress.InProgress

	// If we've processed more than total, something is wrong
	if processed > progress.Total {
		// Force InProgress to 0 and adjust total if needed
		progress.InProgress = 0
		if processed > progress.Total {
			progress.Total = processed
		}
	}

	// If total active exceeds total, reduce InProgress
	if totalActive > progress.Total {
		progress.InProgress = progress.Total - processed
		if progress.InProgress < 0 {
			progress.InProgress = 0
		}
	}
}

// BatchProgress tracks progress for multiple batches
type BatchProgress struct {
	batches map[string]*ProgressTracker
	mutex   sync.RWMutex
}

// NewBatchProgress creates a new batch progress tracker
func NewBatchProgress() *BatchProgress {
	return &BatchProgress{
		batches: make(map[string]*ProgressTracker),
	}
}

// AddBatch adds a new batch to track
func (bp *BatchProgress) AddBatch(batchID string, total int) *ProgressTracker {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	tracker := NewProgressTracker(total)
	bp.batches[batchID] = tracker
	return tracker
}

// GetBatch returns a batch progress tracker
func (bp *BatchProgress) GetBatch(batchID string) *ProgressTracker {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	return bp.batches[batchID]
}

// GetOverallProgress returns combined progress across all batches
func (bp *BatchProgress) GetOverallProgress() *Progress {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	if len(bp.batches) == 0 {
		return NewProgress(0)
	}

	overall := &Progress{
		StartTime: time.Now(), // Will be updated to earliest start time
	}

	for _, tracker := range bp.batches {
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

// RemoveBatch removes a completed batch
func (bp *BatchProgress) RemoveBatch(batchID string) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	if tracker, exists := bp.batches[batchID]; exists {
		tracker.Close()
		delete(bp.batches, batchID)
	}
}

// Close closes all batch trackers
func (bp *BatchProgress) Close() {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	for _, tracker := range bp.batches {
		tracker.Close()
	}
	bp.batches = make(map[string]*ProgressTracker)
}
