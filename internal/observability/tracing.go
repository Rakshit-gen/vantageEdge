package observability

import (
	"context"
	"fmt"
	"time"
)

type TraceSpan struct {
	ID        string
	TraceID   string
	SpanID    string
	ParentID  string
	Operation string
	StartTime time.Time
	EndTime   time.Time
	Status    string
	Error     error
	Tags      map[string]interface{}
}

type Tracer struct {
	traces map[string][]*TraceSpan
}

func NewTracer() *Tracer {
	return &Tracer{
		traces: make(map[string][]*TraceSpan),
	}
}

// StartSpan creates a new trace span
func (t *Tracer) StartSpan(ctx context.Context, traceID, operation string) *TraceSpan {
	span := &TraceSpan{
		TraceID:   traceID,
		SpanID:    generateSpanID(),
		Operation: operation,
		StartTime: time.Now(),
		Status:    "running",
		Tags:      make(map[string]interface{}),
	}

	if parentID := ctx.Value("parent_span_id"); parentID != nil {
		span.ParentID = parentID.(string)
	}

	return span
}

// EndSpan marks a span as complete
func (t *Tracer) EndSpan(span *TraceSpan, err error) {
	span.EndTime = time.Now()
	if err != nil {
		span.Status = "error"
		span.Error = err
	} else {
		span.Status = "success"
	}
}

// AddTag adds a tag to a span
func (span *TraceSpan) AddTag(key string, value interface{}) {
	span.Tags[key] = value
}

// GetDurationMs returns the span duration in milliseconds
func (span *TraceSpan) GetDurationMs() float64 {
	if span.EndTime.IsZero() {
		span.EndTime = time.Now()
	}
	return span.EndTime.Sub(span.StartTime).Seconds() * 1000
}

// generateSpanID generates a unique span ID
func generateSpanID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ExportSpan exports a span for storage/analysis
func (span *TraceSpan) Export() map[string]interface{} {
	return map[string]interface{}{
		"trace_id":   span.TraceID,
		"span_id":    span.SpanID,
		"parent_id":  span.ParentID,
		"operation":  span.Operation,
		"start_time": span.StartTime.Unix(),
		"end_time":   span.EndTime.Unix(),
		"duration_ms": span.GetDurationMs(),
		"status":     span.Status,
		"error":      span.Error,
		"tags":       span.Tags,
	}
}
