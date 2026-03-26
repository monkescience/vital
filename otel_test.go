package vital_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkescience/vital"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

// ExampleTracing demonstrates using tracing middleware.
func ExampleTracing() {
	// Create trace provider
	tp := sdktrace.NewTracerProvider()

	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	tracingHandler := vital.Tracing(vital.WithTracerProvider(tp))(handler)

	fmt.Println("Handler instrumented with tracing")

	// Cleanup
	_ = tracingHandler

	// Output:
	// Handler instrumented with tracing
}

// ExampleMetrics demonstrates using metrics middleware.
func ExampleMetrics() {
	// Create meter provider
	mp := sdkmetric.NewMeterProvider()

	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with metrics middleware
	metricsMiddleware, err := vital.Metrics(vital.WithMeterProvider(mp))
	if err != nil {
		panic(err)
	}

	metricsHandler := metricsMiddleware(handler)

	fmt.Println("Handler instrumented with metrics")

	// Cleanup
	_ = metricsHandler

	// Output:
	// Handler instrumented with metrics
}

func TestTracing(t *testing.T) {
	t.Run("creates span for each request", func(t *testing.T) {
		// given: tracing middleware with trace provider
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})

		wrappedHandler := vital.Tracing(vital.WithTracerProvider(tp))(handler)

		// when: processing an HTTP request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/123", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: a span should be created with standard HTTP attributes
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if span.SpanKind != trace.SpanKindServer {
			t.Errorf("expected SpanKindServer, got %v", span.SpanKind)
		}

		attrs := make(map[string]any)
		for _, attr := range span.Attributes {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}

		if attrs[string(semconv.HTTPRequestMethodKey)] != "GET" {
			t.Errorf("expected method GET, got %v", attrs[string(semconv.HTTPRequestMethodKey)])
		}

		if attrs[string(semconv.HTTPResponseStatusCodeKey)] != int64(http.StatusCreated) {
			t.Errorf("expected status 201, got %v", attrs[string(semconv.HTTPResponseStatusCodeKey)])
		}

		if attrs[string(semconv.URLPathKey)] != "/users/123" {
			t.Errorf("expected path /users/123, got %v", attrs[string(semconv.URLPathKey)])
		}
	})

	t.Run("propagates incoming W3C traceparent", func(t *testing.T) {
		// given: tracing middleware with trace provider and W3C propagator
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := vital.Tracing(
			vital.WithTracerProvider(tp),
			vital.WithPropagator(propagation.TraceContext{}),
		)(handler)

		// when: request has incoming traceparent header
		incomingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
		incomingSpanID := "00f067aa0ba902b7"
		incomingTraceparent := "00-" + incomingTraceID + "-" + incomingSpanID + "-01"

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		req.Header.Set("Traceparent", incomingTraceparent)

		rec := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rec, req)

		// then: span should use the incoming trace ID as parent trace
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if span.SpanContext.TraceID().String() != incomingTraceID {
			t.Errorf("expected trace ID %s, got %s", incomingTraceID, span.SpanContext.TraceID().String())
		}

		if span.SpanContext.SpanID().String() == incomingSpanID {
			t.Error("expected child span with new span ID")
		}

		if !span.Parent.IsValid() {
			t.Error("expected valid parent span context")
		}

		if !span.Parent.IsRemote() {
			t.Error("expected remote parent span context")
		}
	})

	t.Run("adds trace context to request context", func(t *testing.T) {
		// given: tracing middleware with trace provider
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		var capturedTraceID, capturedSpanID, capturedTraceFlags string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedTraceID = vital.GetTraceID(r.Context())
			capturedSpanID = vital.GetSpanID(r.Context())
			capturedTraceFlags = vital.GetTraceFlags(r.Context())

			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := vital.Tracing(vital.WithTracerProvider(tp))(handler)

		// when: processing request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: trace context values should be available in request context
		if capturedTraceID == "" {
			t.Error("expected non-empty trace ID")
		}

		if capturedSpanID == "" {
			t.Error("expected non-empty span ID")
		}

		if capturedTraceFlags == "" {
			t.Error("expected non-empty trace flags")
		}

		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if capturedTraceID != span.SpanContext.TraceID().String() {
			t.Errorf("expected trace ID %s, got %s", span.SpanContext.TraceID().String(), capturedTraceID)
		}

		if capturedSpanID != span.SpanContext.SpanID().String() {
			t.Errorf("expected span ID %s, got %s", span.SpanContext.SpanID().String(), capturedSpanID)
		}
	})

	t.Run("does not propagate traceparent to response", func(t *testing.T) {
		// given: tracing middleware with trace provider and propagator
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := vital.Tracing(
			vital.WithTracerProvider(tp),
			vital.WithPropagator(propagation.TraceContext{}),
		)(handler)

		// when: processing request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: response should not expose trace headers back to the client
		traceparent := rec.Header().Get("Traceparent")
		if traceparent != "" {
			t.Fatalf("expected no traceparent header in response, got %q", traceparent)
		}
	})

	t.Run("marks 5xx responses as span errors", func(t *testing.T) {
		// given: tracing middleware with a failing handler
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		wrappedHandler := vital.Tracing(vital.WithTracerProvider(tp))(handler)

		// when: processing the failing request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		if spans[0].Status.Code != codes.Error {
			t.Errorf("expected span status %v, got %v", codes.Error, spans[0].Status.Code)
		}
	})

	t.Run("handles invalid traceparent gracefully", func(t *testing.T) {
		// given: tracing middleware and invalid traceparent values
		invalidTraceparents := []string{
			"invalid-format",
			"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
			"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		}

		for _, traceparent := range invalidTraceparents {
			t.Run(traceparent, func(t *testing.T) {
				spanExporter := tracetest.NewInMemoryExporter()
				tp := sdktrace.NewTracerProvider(
					sdktrace.WithSyncer(spanExporter),
				)

				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})

				wrappedHandler := vital.Tracing(
					vital.WithTracerProvider(tp),
					vital.WithPropagator(propagation.TraceContext{}),
				)(handler)

				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
				req.Header.Set("Traceparent", traceparent)

				rec := httptest.NewRecorder()
				wrappedHandler.ServeHTTP(rec, req)

				// then: request should still succeed with a valid new trace
				if rec.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", rec.Code)
				}

				spans := spanExporter.GetSpans()
				if len(spans) != 1 {
					t.Fatalf("expected 1 span, got %d", len(spans))
				}

				if !spans[0].SpanContext.TraceID().IsValid() {
					t.Error("expected valid trace ID")
				}
			})
		}
	})
}

func TestMetrics(t *testing.T) {
	t.Run("records HTTP request duration metrics", func(t *testing.T) {
		// given: metrics middleware with manual metric reader
		ctx := context.Background()
		metricReader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(metricReader),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		})

		metricsMiddleware := mustMetricsMiddleware(t, vital.WithMeterProvider(meterProvider))
		wrappedHandler := metricsMiddleware(handler)

		// when: processing multiple requests
		for range 3 {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users", nil)
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}

		// then: duration histogram should be recorded
		var resourceMetrics metricdata.ResourceMetrics

		err := metricReader.Collect(ctx, &resourceMetrics)
		if err != nil {
			t.Fatalf("failed to collect metrics: %v", err)
		}

		metric := findMetricByName(resourceMetrics.ScopeMetrics, "http.server.request.duration")
		if metric == nil {
			t.Fatal("expected http.server.request.duration metric")
		}

		histogram, ok := metric.Data.(metricdata.Histogram[float64])
		if !ok {
			t.Fatalf("expected Histogram[float64], got %T", metric.Data)
		}

		if len(histogram.DataPoints) == 0 {
			t.Fatal("expected histogram data points")
		}

		dataPoint := histogram.DataPoints[0]
		if dataPoint.Count != 3 {
			t.Errorf("expected count 3, got %d", dataPoint.Count)
		}

		if dataPoint.Sum <= 0 {
			t.Errorf("expected positive duration sum, got %f", dataPoint.Sum)
		}

		assertAttributeValue(t, dataPoint.Attributes.ToSlice(), semconv.HTTPRequestMethodKey, "GET")
	})

	t.Run("works with default meter provider", func(t *testing.T) {
		// given: metrics middleware without explicit provider
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		metricsMiddleware := mustMetricsMiddleware(t)
		wrappedHandler := metricsMiddleware(handler)

		// when: processing request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: request should complete successfully
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body success, got %q", rec.Body.String())
		}
	})
}

func TestTracingAndMetricsChain(t *testing.T) {
	t.Run("works in middleware chain and correlates logs", func(t *testing.T) {
		// given: tracing and metrics middlewares with context-aware logger
		spanExporter := tracetest.NewInMemoryExporter()
		tracerProvider := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		metricReader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(metricReader),
		)

		var logBuffer bytes.Buffer

		logger := slog.New(vital.NewContextHandler(
			slog.NewJSONHandler(&logBuffer, nil),
			vital.WithBuiltinKeys(),
		))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		metricsMiddleware := mustMetricsMiddleware(t, vital.WithMeterProvider(meterProvider))

		// Chain: Recovery -> Tracing -> Metrics -> RequestLogger -> Handler
		wrappedHandler := vital.Recovery(logger)(
			vital.Tracing(vital.WithTracerProvider(tracerProvider))(
				metricsMiddleware(
					vital.RequestLogger(logger)(handler),
				),
			),
		)

		// when: processing request
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: request should succeed and tracing, metrics, and logs should all be populated
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body success, got %q", rec.Body.String())
		}

		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		var resourceMetrics metricdata.ResourceMetrics

		err := metricReader.Collect(context.Background(), &resourceMetrics)
		if err != nil {
			t.Fatalf("failed to collect metrics: %v", err)
		}

		metric := findMetricByName(resourceMetrics.ScopeMetrics, "http.server.request.duration")
		if metric == nil {
			t.Fatal("expected http.server.request.duration metric")
		}

		logOutput := logBuffer.String()
		if !strings.Contains(logOutput, `"trace_id":"`) {
			t.Errorf("expected trace_id in request log, got: %s", logOutput)
		}

		if !strings.Contains(logOutput, `"span_id":"`) {
			t.Errorf("expected span_id in request log, got: %s", logOutput)
		}
	})
}

func findMetricByName(scopeMetrics []metricdata.ScopeMetrics, name string) *metricdata.Metrics {
	for _, scopeMetric := range scopeMetrics {
		for idx := range scopeMetric.Metrics {
			if scopeMetric.Metrics[idx].Name == name {
				return &scopeMetric.Metrics[idx]
			}
		}
	}

	return nil
}

func mustMetricsMiddleware(t *testing.T, opts ...vital.MetricsOption) vital.Middleware {
	t.Helper()

	middleware, err := vital.Metrics(opts...)
	if err != nil {
		t.Fatalf("failed to create metrics middleware: %v", err)
	}

	return middleware
}

func BenchmarkMiddlewareChain(b *testing.B) {
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(spanExporter),
	)

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
	)

	logger := slog.New(slog.NewJSONHandler(nopWriter{}, nil))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	metricsMiddleware, err := vital.Metrics(vital.WithMeterProvider(meterProvider))
	if err != nil {
		b.Fatalf("failed to create metrics middleware: %v", err)
	}

	chain := vital.Recovery(logger)(
		vital.Tracing(vital.WithTracerProvider(tracerProvider))(
			metricsMiddleware(
				vital.RequestLogger(logger)(handler),
			),
		),
	)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
	}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func assertAttributeValue(t *testing.T, attrs []attribute.KeyValue, key attribute.Key, expected string) {
	t.Helper()

	for _, attr := range attrs {
		if attr.Key == key {
			if attr.Value.AsString() != expected {
				t.Errorf("attribute %s: expected %q, got %q", key, expected, attr.Value.AsString())
			}

			return
		}
	}

	t.Errorf("expected attribute %s not found", key)
}
