package vital

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/monkescience/vital"
)

// TracingOption configures tracing middleware.
type TracingOption func(*tracingConfig)

// MetricsOption configures metrics middleware.
type MetricsOption func(*metricsConfig)

type tracingConfig struct {
	tracerProvider trace.TracerProvider
	propagator     propagation.TextMapPropagator
}

type metricsConfig struct {
	meterProvider metric.MeterProvider
}

// WithTracerProvider sets a custom tracer provider.
func WithTracerProvider(tp trace.TracerProvider) TracingOption {
	return func(c *tracingConfig) {
		c.tracerProvider = tp
	}
}

// WithMeterProvider sets a custom meter provider.
func WithMeterProvider(mp metric.MeterProvider) MetricsOption {
	return func(c *metricsConfig) {
		c.meterProvider = mp
	}
}

// WithPropagator sets a custom propagator (default W3C TraceContext).
func WithPropagator(p propagation.TextMapPropagator) TracingOption {
	return func(c *tracingConfig) {
		c.propagator = p
	}
}

func newTracingConfig(opts ...TracingOption) tracingConfig {
	cfg := tracingConfig{propagator: propagation.TraceContext{}}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.tracerProvider == nil {
		cfg.tracerProvider = otel.GetTracerProvider()
	}

	return cfg
}

func newMetricsConfig(opts ...MetricsOption) metricsConfig {
	cfg := metricsConfig{}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.meterProvider == nil {
		cfg.meterProvider = otel.GetMeterProvider()
	}

	return cfg
}

// Tracing returns a middleware that instruments HTTP requests with OpenTelemetry traces.
//
// Features:
//   - Creates a span for each HTTP request with standard HTTP semantic conventions
//   - Propagates inbound W3C traceparent headers
//   - Adds trace_id, span_id, and trace_flags to request context for log correlation
func Tracing(opts ...TracingOption) Middleware {
	cfg := newTracingConfig(opts...)
	tracer := cfg.tracerProvider.Tracer(instrumentationName)

	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := cfg.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := tracer.Start(ctx, "HTTP "+r.Method, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			if spanCtx := span.SpanContext(); spanCtx.IsValid() {
				ctx = context.WithValue(ctx, TraceIDKey, spanCtx.TraceID().String())
				ctx = context.WithValue(ctx, SpanIDKey, spanCtx.SpanID().String())
				ctx = context.WithValue(ctx, TraceFlagsKey, spanCtx.TraceFlags().String())
			}

			wrapped := wrapResponseWriter(w)

			next.ServeHTTP(wrapped, r.WithContext(ctx))

			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.HTTPResponseStatusCodeKey.Int(wrapped.statusCode),
				semconv.URLPathKey.String(r.URL.Path),
			)

			if wrapped.statusCode >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(wrapped.statusCode))
			}
		})
	}
}

// Metrics returns a middleware that records OpenTelemetry HTTP server metrics.
// Returns an error if required metric instruments cannot be created.
//
// Features:
//   - Records HTTP metrics: http.server.request.duration histogram
func Metrics(opts ...MetricsOption) (Middleware, error) {
	cfg := newMetricsConfig(opts...)
	meter := cfg.meterProvider.Meter(instrumentationName)

	durationHistogram, histogramErr := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests"),
		metric.WithUnit("s"),
	)
	if histogramErr != nil {
		return nil, fmt.Errorf("create request duration histogram: %w", histogramErr)
	}

	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := wrapResponseWriter(w)

			start := time.Now()

			next.ServeHTTP(wrapped, r)

			recordDurationMetric(r.Context(), r, wrapped.statusCode, durationHistogram, start)
		})
	}, nil
}

func recordDurationMetric(
	ctx context.Context,
	r *http.Request,
	statusCode int,
	histogram metric.Float64Histogram,
	start time.Time,
) {
	duration := time.Since(start).Seconds()
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(r.Method),
		semconv.HTTPResponseStatusCodeKey.Int(statusCode),
	}

	histogram.Record(ctx, duration, metric.WithAttributes(attrs...))
}
