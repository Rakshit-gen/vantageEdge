package slidingwindow

import (
	"sync"
	"time"
)

type SlidingWindow struct {
	mu              sync.Mutex
	maxRequests     int
	windowSize      time.Duration
	requestTimings  []time.Time
}

func NewSlidingWindow(maxRequests int, windowSize time.Duration) *SlidingWindow {
	return &SlidingWindow{
		maxRequests:    maxRequests,
		windowSize:     windowSize,
		requestTimings: make([]time.Time, 0, maxRequests),
	}
}

// Allow checks if a request is allowed
func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.windowSize)

	// Remove old request timings outside the window
	validRequests := make([]time.Time, 0, len(sw.requestTimings))
	for _, reqTime := range sw.requestTimings {
		if reqTime.After(windowStart) {
			validRequests = append(validRequests, reqTime)
		}
	}
	sw.requestTimings = validRequests

	// Check if we can allow this request
	if len(sw.requestTimings) < sw.maxRequests {
		sw.requestTimings = append(sw.requestTimings, now)
		return true
	}

	return false
}

// AllowN allows n requests at once
func (sw *SlidingWindow) AllowN(n int) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.windowSize)

	// Remove old request timings
	validRequests := make([]time.Time, 0, len(sw.requestTimings))
	for _, reqTime := range sw.requestTimings {
		if reqTime.After(windowStart) {
			validRequests = append(validRequests, reqTime)
		}
	}

	// Check if we can allow n requests
	if len(validRequests)+n <= sw.maxRequests {
		for i := 0; i < n; i++ {
			validRequests = append(validRequests, now)
		}
		sw.requestTimings = validRequests
		return true
	}

	sw.requestTimings = validRequests
	return false
}

// GetRemainingRequests returns the number of requests allowed in the current window
func (sw *SlidingWindow) GetRemainingRequests() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.windowSize)

	// Count valid requests
	validCount := 0
	for _, reqTime := range sw.requestTimings {
		if reqTime.After(windowStart) {
			validCount++
		}
	}

	return sw.maxRequests - validCount
}

// Reset clears all request timings
func (sw *SlidingWindow) Reset() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.requestTimings = make([]time.Time, 0, sw.maxRequests)
}
