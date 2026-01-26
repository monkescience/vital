package vital

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/monkescience/vital"
)

// OTelOption configures the OTel middleware.
type OTelOption func(*otelConfig)

// otelConfig holds configuration for the OTel middleware.
type otelConfig struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	propagator     propagation.TextMapPropagator
}

// WithTracerProvider sets a custom tracer provider.
func WithTracerProvider(tp trace.TracerProvider) OTelOption {
	return func(c *otelConfig) {
		c.tracerProvider = tp
	}
}

// WithMeterProvider sets a custom meter provider.
func WithMeterProvider(mp metric.MeterProvider) OTelOption {
	return func(c *otelConfig) {
		c.meterProvider = mp
	}
}

// WithPropagator sets a custom propagator (default W3C TraceContext).
func WithPropagator(p propagation.TextMapPropagator) OTelOption {
	return func(c *otelConfig) {
		c.propagator = p
	}
}

// OTel returns a middleware that instruments HTTP requests with OpenTelemetry traces and metrics.
//
// Features:
//   - Creates a span for each HTTP request with standard HTTP semantic conventions
//   - Propagates W3C traceparent headers (incoming and outgoing)
//   - Records HTTP metrics: http.server.request.duration histogram
//   - Adds trace_id and span_id to request context for log correlation
//   - Gracefully degrades if providers not configured (no-op mode)
//
// Example:
//
//	tp := sdktrace.NewTracerProvider(...)
//	mp := sdkmetric.NewMeterProvider(...)
//	handler := vital.OTel(
//	    vital.WithTracerProvider(tp),
//	    vital.WithMeterProvider(mp),
//	)(myHandler)
func OTel(opts ...OTelOption) Middleware {
	cfg := &otelConfig{
		propagator: propagation.TraceContext{},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.tracerProvider == nil {
		cfg.tracerProvider = otel.GetTracerProvider()
	}
	if cfg.meterProvider == nil {
		cfg.meterProvider = otel.GetMeterProvider()
	}

	tracer := cfg.tracerProvider.Tracer(instrumentationName)
	meter := cfg.meterProvider.Meter(instrumentationName)

	durationHistogram, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP server requests"),
		metric.WithUnit("s"),
	)

	return func(next http.Handler) http.Handler {
		//nolint:varnamelen // w and r are conventional names for http.ResponseWriter and *http.Request
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := cfg.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := tracer.Start(ctx, "HTTP "+r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			spanContext := span.SpanContext()
			if spanContext.IsValid() {
				ctx = context.WithValue(ctx, TraceIDKey, spanContext.TraceID().String())
				ctx = context.WithValue(ctx, SpanIDKey, spanContext.SpanID().String())
			}

			r = r.WithContext(ctx)

			rw := &otelResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			start := time.Now()

			cfg.propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Seconds()
			metricAttrs := []attribute.KeyValue{
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.HTTPResponseStatusCodeKey.Int(rw.statusCode),
			}
			durationHistogram.Record(ctx, duration, metric.WithAttributes(metricAttrs...))

			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.HTTPResponseStatusCodeKey.Int(rw.statusCode),
				semconv.URLPathKey.String(r.URL.Path),
			)
		})
	}
}

// otelResponseWriter wraps http.ResponseWriter to capture status code.
type otelResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code.
func (rw *otelResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
