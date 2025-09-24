package bitbucket

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter defines the interface for rate limiting
type RateLimiter interface {
	// Wait blocks until a request can be made
	Wait(ctx context.Context) error

	// Allow checks if a request can be made immediately
	Allow() bool

	// UpdateRemaining updates the remaining request count
	UpdateRemaining(remaining int)

	// UpdateResetTime updates when the rate limit resets
	UpdateResetTime(resetTime time.Time)
}

// TokenBucketRateLimiter implements a token bucket rate limiter for Bitbucket
type TokenBucketRateLimiter struct {
	mu         sync.Mutex
	limit      int       // Maximum number of requests per hour
	remaining  int       // Remaining requests
	resetTime  time.Time // When the rate limit resets
	lastRefill time.Time // Last time tokens were refilled
	tokens     float64   // Current number of tokens
	refillRate float64   // Tokens per second
}

// NewTokenBucketRateLimiter creates a new token bucket rate limiter for Bitbucket
// Bitbucket's default rate limit is 1000 requests per hour for authenticated users
func NewTokenBucketRateLimiter(limit int) *TokenBucketRateLimiter {
	now := time.Now()
	if limit == 0 {
		limit = 1000 // Bitbucket's default
	}

	return &TokenBucketRateLimiter{
		limit:      limit,
		remaining:  limit,
		resetTime:  now.Add(time.Hour),
		lastRefill: now,
		tokens:     float64(limit),
		refillRate: float64(limit) / 3600.0, // tokens per second
	}
}

// Wait blocks until a request can be made
func (r *TokenBucketRateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refillTokens()

	// If we have tokens, consume one and return
	if r.tokens >= 1.0 {
		r.tokens -= 1.0
		return nil
	}

	// Calculate how long to wait for the next token
	waitTime := time.Duration((1.0 - r.tokens) / r.refillRate * float64(time.Second))

	// Wait with context cancellation
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	select {
	case <-timer.C:
		r.refillTokens()
		if r.tokens >= 1.0 {
			r.tokens -= 1.0
			return nil
		}
		return fmt.Errorf("rate limit exceeded")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Allow checks if a request can be made immediately
func (r *TokenBucketRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refillTokens()

	if r.tokens >= 1.0 {
		r.tokens -= 1.0
		return true
	}

	return false
}

// UpdateRemaining updates the remaining request count from Bitbucket headers
func (r *TokenBucketRateLimiter) UpdateRemaining(remaining int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.remaining = remaining

	// Adjust tokens based on Bitbucket's actual remaining count
	// This helps sync our local state with Bitbucket's rate limiter
	if remaining < int(r.tokens) {
		r.tokens = float64(remaining)
	}
}

// UpdateResetTime updates when the rate limit resets
func (r *TokenBucketRateLimiter) UpdateResetTime(resetTime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resetTime = resetTime

	// If reset time has passed, refill tokens
	if time.Now().After(resetTime) {
		r.tokens = float64(r.limit)
		r.lastRefill = time.Now()
	}
}

// refillTokens refills the token bucket based on elapsed time
func (r *TokenBucketRateLimiter) refillTokens() {
	now := time.Now()

	// If reset time has passed, fully refill
	if now.After(r.resetTime) {
		r.tokens = float64(r.limit)
		r.lastRefill = now
		r.resetTime = now.Add(time.Hour)
		return
	}

	// Calculate tokens to add based on elapsed time
	elapsed := now.Sub(r.lastRefill).Seconds()
	tokensToAdd := elapsed * r.refillRate

	r.tokens = min(r.tokens+tokensToAdd, float64(r.limit))
	r.lastRefill = now
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// NoOpRateLimiter is a rate limiter that doesn't limit anything
type NoOpRateLimiter struct{}

// NewNoOpRateLimiter creates a no-op rate limiter
func NewNoOpRateLimiter() *NoOpRateLimiter {
	return &NoOpRateLimiter{}
}

// Wait always returns immediately
func (r *NoOpRateLimiter) Wait(ctx context.Context) error {
	return nil
}

// Allow always returns true
func (r *NoOpRateLimiter) Allow() bool {
	return true
}

// UpdateRemaining does nothing
func (r *NoOpRateLimiter) UpdateRemaining(remaining int) {
	// No-op
}

// UpdateResetTime does nothing
func (r *NoOpRateLimiter) UpdateResetTime(resetTime time.Time) {
	// No-op
}
