package cloning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgress(t *testing.T) {
	total := 10
	progress := NewProgress(total)

	assert.Equal(t, total, progress.Total)
	assert.Equal(t, 0, progress.Completed)
	assert.Equal(t, 0, progress.Failed)
	assert.Equal(t, 0, progress.Skipped)
	assert.Equal(t, 0, progress.InProgress)
	assert.WithinDuration(t, time.Now(), progress.StartTime, time.Second)
}

func TestProgress_GetPercentage(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		completed int
		failed    int
		skipped   int
		expected  float64
	}{
		{
			name:      "empty progress",
			total:     10,
			completed: 0,
			failed:    0,
			skipped:   0,
			expected:  0.0,
		},
		{
			name:      "half complete",
			total:     10,
			completed: 3,
			failed:    1,
			skipped:   1,
			expected:  50.0,
		},
		{
			name:      "fully complete",
			total:     10,
			completed: 6,
			failed:    2,
			skipped:   2,
			expected:  100.0,
		},
		{
			name:      "zero total",
			total:     0,
			completed: 0,
			failed:    0,
			skipped:   0,
			expected:  100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := &Progress{
				Total:     tt.total,
				Completed: tt.completed,
				Failed:    tt.failed,
				Skipped:   tt.skipped,
			}
			assert.Equal(t, tt.expected, progress.GetPercentage())
		})
	}
}

func TestProgress_GetSuccessRate(t *testing.T) {
	tests := []struct {
		name      string
		completed int
		failed    int
		skipped   int
		expected  float64
	}{
		{
			name:      "no processed jobs",
			completed: 0,
			failed:    0,
			skipped:   0,
			expected:  0.0,
		},
		{
			name:      "all successful",
			completed: 10,
			failed:    0,
			skipped:   0,
			expected:  100.0,
		},
		{
			name:      "mixed results",
			completed: 8,
			failed:    2,
			skipped:   0,
			expected:  80.0,
		},
		{
			name:      "with skipped jobs",
			completed: 6,
			failed:    2,
			skipped:   2,
			expected:  60.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := &Progress{
				Completed: tt.completed,
				Failed:    tt.failed,
				Skipped:   tt.skipped,
			}
			assert.Equal(t, tt.expected, progress.GetSuccessRate())
		})
	}
}

func TestProgress_IsComplete(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		completed   int
		failed      int
		skipped     int
		inProgress  int
		expected    bool
	}{
		{
			name:        "not complete",
			total:       10,
			completed:   3,
			failed:      1,
			skipped:     1,
			inProgress:  2,
			expected:    false,
		},
		{
			name:        "complete",
			total:       10,
			completed:   6,
			failed:      2,
			skipped:     2,
			inProgress:  0,
			expected:    true,
		},
		{
			name:        "over complete",
			total:       10,
			completed:   8,
			failed:      2,
			skipped:     2,
			inProgress:  0,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress := &Progress{
				Total:      tt.total,
				Completed:  tt.completed,
				Failed:     tt.failed,
				Skipped:    tt.skipped,
				InProgress: tt.inProgress,
			}
			assert.Equal(t, tt.expected, progress.IsComplete())
		})
	}
}

func TestProgress_CalculateETA(t *testing.T) {
	progress := NewProgress(10)
	progress.StartTime = time.Now().Add(-1 * time.Minute)
	progress.Completed = 5
	
	progress.CalculateETA()
	
	assert.True(t, progress.ETA > 0)
	assert.True(t, progress.Throughput > 0)
}

func TestNewProgressTracker(t *testing.T) {
	total := 5
	tracker := NewProgressTracker(total)

	require.NotNil(t, tracker)
	progress := tracker.GetProgress()
	assert.Equal(t, total, progress.Total)
	assert.Equal(t, 0, progress.Completed)
}

func TestProgressTracker_StartJob(t *testing.T) {
	tracker := NewProgressTracker(5)
	
	tracker.StartJob()
	
	progress := tracker.GetProgress()
	assert.Equal(t, 1, progress.InProgress)
}

func TestProgressTracker_CompleteJob(t *testing.T) {
	tracker := NewProgressTracker(5)
	tracker.StartJob()
	
	tracker.CompleteJob()
	
	progress := tracker.GetProgress()
	assert.Equal(t, 1, progress.Completed)
	assert.Equal(t, 0, progress.InProgress)
}

func TestProgressTracker_FailJob(t *testing.T) {
	tracker := NewProgressTracker(5)
	tracker.StartJob()
	
	tracker.FailJob()
	
	progress := tracker.GetProgress()
	assert.Equal(t, 1, progress.Failed)
	assert.Equal(t, 0, progress.InProgress)
}

func TestProgressTracker_SkipJob(t *testing.T) {
	tracker := NewProgressTracker(5)
	tracker.StartJob()
	
	tracker.SkipJob()
	
	progress := tracker.GetProgress()
	assert.Equal(t, 1, progress.Skipped)
	assert.Equal(t, 0, progress.InProgress)
}

func TestProgressTracker_Subscribe(t *testing.T) {
	tracker := NewProgressTracker(5)
	
	updates := tracker.Subscribe()
	
	// Start a job to trigger an update
	tracker.StartJob()
	
	// Should receive an update
	select {
	case progress := <-updates:
		assert.Equal(t, 1, progress.InProgress)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected progress update")
	}
}

func TestProgressTracker_Close(t *testing.T) {
	tracker := NewProgressTracker(5)
	updates := tracker.Subscribe()
	
	tracker.Close()
	
	// Channel should be closed
	select {
	case _, ok := <-updates:
		assert.False(t, ok, "Channel should be closed")
	case <-time.After(1 * time.Second):
		t.Fatal("Channel should have been closed")
	}
}

func TestNewBatchProgress(t *testing.T) {
	batchProgress := NewBatchProgress()
	
	assert.NotNil(t, batchProgress)
	
	// Should be empty initially
	overall := batchProgress.GetOverallProgress()
	assert.Equal(t, 0, overall.Total)
}

func TestBatchProgress_AddBatch(t *testing.T) {
	batchProgress := NewBatchProgress()
	batchID := "test-batch"
	total := 10
	
	tracker := batchProgress.AddBatch(batchID, total)
	
	assert.NotNil(t, tracker)
	
	// Should be able to retrieve the batch
	retrievedTracker := batchProgress.GetBatch(batchID)
	assert.Equal(t, tracker, retrievedTracker)
}

func TestBatchProgress_GetOverallProgress(t *testing.T) {
	batchProgress := NewBatchProgress()
	
	// Add multiple batches
	tracker1 := batchProgress.AddBatch("batch1", 5)
	tracker2 := batchProgress.AddBatch("batch2", 3)
	
	// Complete some jobs
	tracker1.StartJob()
	tracker1.CompleteJob()
	tracker2.StartJob()
	tracker2.FailJob()
	
	overall := batchProgress.GetOverallProgress()
	
	assert.Equal(t, 8, overall.Total)
	assert.Equal(t, 1, overall.Completed)
	assert.Equal(t, 1, overall.Failed)
}

func TestBatchProgress_RemoveBatch(t *testing.T) {
	batchProgress := NewBatchProgress()
	batchID := "test-batch"
	
	batchProgress.AddBatch(batchID, 5)
	
	batchProgress.RemoveBatch(batchID)
	
	// Should no longer exist
	tracker := batchProgress.GetBatch(batchID)
	assert.Nil(t, tracker)
}

func TestBatchProgress_Close(t *testing.T) {
	batchProgress := NewBatchProgress()
	
	// Add some batches
	batchProgress.AddBatch("batch1", 5)
	batchProgress.AddBatch("batch2", 3)
	
	batchProgress.Close()
	
	// All batches should be removed
	overall := batchProgress.GetOverallProgress()
	assert.Equal(t, 0, overall.Total)
}

// Test concurrent access to progress tracker
func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewProgressTracker(100)
	
	// Start multiple goroutines that update progress
	done := make(chan bool)
	
	// Workers completing jobs
	go func() {
		for i := 0; i < 50; i++ {
			tracker.StartJob()
			tracker.CompleteJob()
		}
		done <- true
	}()
	
	// Workers failing jobs
	go func() {
		for i := 0; i < 30; i++ {
			tracker.StartJob()
			tracker.FailJob()
		}
		done <- true
	}()
	
	// Workers skipping jobs
	go func() {
		for i := 0; i < 20; i++ {
			tracker.StartJob()
			tracker.SkipJob()
		}
		done <- true
	}()
	
	// Wait for all workers to complete
	for i := 0; i < 3; i++ {
		<-done
	}
	
	progress := tracker.GetProgress()
	assert.Equal(t, 50, progress.Completed)
	assert.Equal(t, 30, progress.Failed)
	assert.Equal(t, 20, progress.Skipped)
	assert.Equal(t, 0, progress.InProgress)
	assert.True(t, progress.IsComplete())
}