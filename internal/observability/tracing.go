package observability

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracing wires this process's spans to Jaeger over OTLP/HTTP.
//
// docker-compose's Jaeger service was previously receiving nothing: the
// package's old Tracer type only recorded spans into an in-memory map that
// was never read by anything, and nothing in cmd/*/main.go ever
// constructed one. otlpEndpoint should be a host:port (e.g. "jaeger:4318",
// matching the OTLP HTTP port docker-compose now exposes) — not a full
// URL, since otlptracehttp builds the path itself.
//
// Returns a shutdown func that must be called (with a bounded context) on
// process exit to flush any buffered spans, and a no-op shutdown if
// tracing is disabled.
func InitTracing(ctx context.Context, serviceName, otlpEndpoint string, enabled bool) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !enabled {
		return noop, nil
	}
	if otlpEndpoint == "" {
		return noop, fmt.Errorf("OTEL_EXPORTER_ENDPOINT is required when OTEL_ENABLED=true")
	}

	// Accept either a bare host:port or a full URL (docker-compose's
	// previous default was a full Jaeger Thrift collector URL); normalize
	// to just the host:port otlptracehttp expects.
	endpoint := otlpEndpoint
	if u, err := url.Parse(otlpEndpoint); err == nil && u.Host != "" {
		endpoint = u.Host
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return noop, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return noop, fmt.Errorf("failed to build trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return provider.Shutdown, nil
}

// Tracer returns the named tracer from the globally configured provider
// (a no-op tracer if InitTracing was never called or was disabled, per
// the otel API's own contract — callers don't need to branch on whether
// tracing is enabled).
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// RequestAttributes are the common span attributes both services attach to
// every request span.
func RequestAttributes(tenantID, routeID, method, path string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("tenant.id", tenantID),
		attribute.String("http.method", method),
		attribute.String("http.path", path),
	}
	if routeID != "" {
		attrs = append(attrs, attribute.String("route.id", routeID))
	}
	return attrs
}
