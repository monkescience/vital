package vital_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monkescience/vital"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

// ExampleOTel demonstrates using OpenTelemetry middleware.
func ExampleOTel() {
	// Create tracer and meter providers
	tp := sdktrace.NewTracerProvider()
	mp := sdkmetric.NewMeterProvider()

	// Create handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with OTel middleware
	otelMiddleware, err := vital.OTel(
		vital.WithTracerProvider(tp),
		vital.WithMeterProvider(mp),
	)
	if err != nil {
		panic(err)
	}

	otelHandler := otelMiddleware(handler)

	fmt.Println("Handler instrumented with OpenTelemetry")

	// Cleanup
	_ = otelHandler

	// Output:
	// Handler instrumented with OpenTelemetry
}

func TestOTel(t *testing.T) {
	t.Run("creates span for each request", func(t *testing.T) {
		// given: OTel middleware with trace provider
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(t, vital.WithTracerProvider(tp))
		wrappedHandler := middleware(handler)

		// when: processing an HTTP request
		req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: a span should be created
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Errorf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if span.SpanKind != trace.SpanKindServer {
			t.Errorf("expected SpanKindServer, got %v", span.SpanKind)
		}
	})

	t.Run("span has standard HTTP attributes", func(t *testing.T) {
		tests := []struct {
			name          string
			method        string
			path          string
			statusCode    int
			expectedAttrs map[string]any
		}{
			{
				name:       "GET request with 200 OK",
				method:     http.MethodGet,
				path:       "/users/123",
				statusCode: http.StatusOK,
				expectedAttrs: map[string]any{
					string(semconv.HTTPRequestMethodKey):      "GET",
					string(semconv.HTTPResponseStatusCodeKey): int64(200),
					string(semconv.URLPathKey):                "/users/123",
				},
			},
			{
				name:       "POST request with 201 Created",
				method:     http.MethodPost,
				path:       "/users",
				statusCode: http.StatusCreated,
				expectedAttrs: map[string]any{
					string(semconv.HTTPRequestMethodKey):      "POST",
					string(semconv.HTTPResponseStatusCodeKey): int64(201),
					string(semconv.URLPathKey):                "/users",
				},
			},
			{
				name:       "DELETE request with 404 Not Found",
				method:     http.MethodDelete,
				path:       "/users/999",
				statusCode: http.StatusNotFound,
				expectedAttrs: map[string]any{
					string(semconv.HTTPRequestMethodKey):      "DELETE",
					string(semconv.HTTPResponseStatusCodeKey): int64(404),
					string(semconv.URLPathKey):                "/users/999",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: OTel middleware with trace provider
				spanExporter := tracetest.NewInMemoryExporter()
				tp := sdktrace.NewTracerProvider(
					sdktrace.WithSyncer(spanExporter),
				)

				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
				})

				middleware := mustOTelMiddleware(t, vital.WithTracerProvider(tp))
				wrappedHandler := middleware(handler)

				// when: processing the request
				req := httptest.NewRequest(tt.method, tt.path, nil)
				rec := httptest.NewRecorder()

				wrappedHandler.ServeHTTP(rec, req)

				// then: span should have standard HTTP semconv attributes
				spans := spanExporter.GetSpans()
				if len(spans) != 1 {
					t.Fatalf("expected 1 span, got %d", len(spans))
				}

				span := spans[0]

				attrs := make(map[string]any)
				for _, attr := range span.Attributes {
					attrs[string(attr.Key)] = attr.Value.AsInterface()
				}

				for key, expectedValue := range tt.expectedAttrs {
					actualValue, exists := attrs[key]
					if !exists {
						t.Errorf("expected attribute %q not found in span", key)

						continue
					}

					if actualValue != expectedValue {
						t.Errorf("attribute %q: expected %v, got %v", key, expectedValue, actualValue)
					}
				}
			})
		}
	})

	t.Run("propagates incoming W3C traceparent", func(t *testing.T) {
		// given: OTel middleware with trace provider and W3C propagator
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		propagator := propagation.TraceContext{}

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(
			t,
			vital.WithTracerProvider(tp),
			vital.WithPropagator(propagator),
		)
		wrappedHandler := middleware(handler)

		// when: request has incoming traceparent header
		incomingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
		incomingSpanID := "00f067aa0ba902b7"
		incomingTraceparent := "00-" + incomingTraceID + "-" + incomingSpanID + "-01"

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Traceparent", incomingTraceparent)

		rec := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rec, req)

		// then: span should have the same trace ID (child span)
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if span.SpanContext.TraceID().String() != incomingTraceID {
			t.Errorf("expected trace ID %s, got %s", incomingTraceID, span.SpanContext.TraceID().String())
		}

		// Span ID should be different (new child span)
		if span.SpanContext.SpanID().String() == incomingSpanID {
			t.Error("expected new span ID (child), got same span ID as parent")
		}

		// Parent should be set
		if !span.Parent.IsValid() {
			t.Error("expected valid parent span context")
		}

		if !span.Parent.IsRemote() {
			t.Error("expected parent to be remote")
		}
	})

	t.Run("generates new trace if no traceparent", func(t *testing.T) {
		// given: OTel middleware without incoming traceparent
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(t, vital.WithTracerProvider(tp))
		wrappedHandler := middleware(handler)

		// when: processing request without traceparent
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: new trace should be generated
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		if !span.SpanContext.TraceID().IsValid() {
			t.Error("expected valid trace ID")
		}

		if !span.SpanContext.SpanID().IsValid() {
			t.Error("expected valid span ID")
		}

		// Should not have parent (root span)
		if span.Parent.IsValid() {
			t.Error("expected no parent for root span")
		}
	})

	t.Run("records HTTP metrics", func(t *testing.T) {
		// given: OTel middleware with meter provider
		ctx := context.Background()
		metricReader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(metricReader),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond) // Simulate work
			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(t, vital.WithMeterProvider(mp))
		wrappedHandler := middleware(handler)

		// when: processing multiple requests
		for range 3 {
			req := httptest.NewRequest(http.MethodGet, "/users", nil)
			rec := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rec, req)
		}

		// then: metrics should be recorded
		var rm metricdata.ResourceMetrics

		err := metricReader.Collect(ctx, &rm)
		if err != nil {
			t.Fatalf("failed to collect metrics: %v", err)
		}

		if len(rm.ScopeMetrics) == 0 {
			t.Fatal("expected scope metrics, got none")
		}

		// Find http.server.request.duration histogram
		metric := findMetricByName(rm.ScopeMetrics, "http.server.request.duration")
		if metric == nil {
			t.Fatal("expected http.server.request.duration metric")
		}

		// Should be a histogram
		histogram, ok := metric.Data.(metricdata.Histogram[float64])
		if !ok {
			t.Fatalf("expected Histogram[float64], got %T", metric.Data)
		}

		// Should have data points
		if len(histogram.DataPoints) == 0 {
			t.Fatal("expected histogram data points, got none")
		}

		// Check first data point
		dp := histogram.DataPoints[0]
		if dp.Count != 3 {
			t.Errorf("expected count 3, got %d", dp.Count)
		}

		if dp.Sum <= 0 {
			t.Errorf("expected positive sum, got %f", dp.Sum)
		}

		// Should have http.request.method attribute
		assertAttributeValue(t, dp.Attributes.ToSlice(), semconv.HTTPRequestMethodKey, "GET")
	})

	t.Run("no-op mode when tracer not configured", func(t *testing.T) {
		// given: OTel middleware without tracer provider
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		middleware := mustOTelMiddleware(t) // No providers configured
		wrappedHandler := middleware(handler)

		// when: processing request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// then: should not panic or error
		wrappedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body 'success', got %q", rec.Body.String())
		}
	})

	t.Run("no-op mode when meter not configured", func(t *testing.T) {
		// given: OTel middleware without meter provider
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		middleware := mustOTelMiddleware(t) // No providers configured
		wrappedHandler := middleware(handler)

		// when: processing request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// then: should not panic or error
		wrappedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body 'success', got %q", rec.Body.String())
		}
	})

	t.Run("integrates with context handler for log correlation", func(t *testing.T) {
		// given: OTel middleware with trace provider
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		var capturedTraceID, capturedSpanID, capturedTraceFlags string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from request context
			capturedTraceID = vital.GetTraceID(r.Context())
			capturedSpanID = vital.GetSpanID(r.Context())
			capturedTraceFlags = vital.GetTraceFlags(r.Context())

			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(t, vital.WithTracerProvider(tp))
		wrappedHandler := middleware(handler)

		// when: processing request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: trace_id and span_id should be in context
		if capturedTraceID == "" {
			t.Error("expected trace_id in context, got empty string")
		}

		if capturedSpanID == "" {
			t.Error("expected span_id in context, got empty string")
		}

		if capturedTraceFlags == "" {
			t.Error("expected trace_flags in context, got empty string")
		}

		// Verify they match the span
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		expectedTraceID := span.SpanContext.TraceID().String()
		expectedSpanID := span.SpanContext.SpanID().String()
		expectedTraceFlags := span.SpanContext.TraceFlags().String()

		if capturedTraceID != expectedTraceID {
			t.Errorf("trace_id mismatch: expected %s, got %s", expectedTraceID, capturedTraceID)
		}

		if capturedSpanID != expectedSpanID {
			t.Errorf("span_id mismatch: expected %s, got %s", expectedSpanID, capturedSpanID)
		}

		if capturedTraceFlags != expectedTraceFlags {
			t.Errorf("trace_flags mismatch: expected %s, got %s", expectedTraceFlags, capturedTraceFlags)
		}
	})

	t.Run("propagates traceparent to response", func(t *testing.T) {
		// given: OTel middleware with trace provider and propagator
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		propagator := propagation.TraceContext{}

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := mustOTelMiddleware(
			t,
			vital.WithTracerProvider(tp),
			vital.WithPropagator(propagator),
		)
		wrappedHandler := middleware(handler)

		// when: processing request
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: response should have traceparent header
		traceparent := rec.Header().Get("Traceparent")
		if traceparent == "" {
			t.Error("expected traceparent in response headers")
		}

		// Verify format: 00-{trace-id}-{span-id}-{flags}
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}

		span := spans[0]
		expectedTraceID := span.SpanContext.TraceID().String()

		if traceparent == "" {
			t.Fatal("traceparent header is empty")
		}

		// Traceparent should contain the trace ID
		// Format: 00-{32-hex-trace-id}-{16-hex-span-id}-{2-hex-flags}
		if len(traceparent) < len(expectedTraceID) {
			t.Errorf("traceparent too short: %s", traceparent)
		}
	})

	t.Run("works in middleware chain", func(t *testing.T) {
		// given: OTel middleware in chain with Recovery and RequestLogger
		spanExporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(spanExporter),
		)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		logger := slog.New(slog.DiscardHandler)
		otelMiddleware := mustOTelMiddleware(t, vital.WithTracerProvider(tp))

		// Chain: Recovery -> RequestLogger -> OTel -> Handler
		wrappedHandler := vital.Recovery(logger)(
			vital.RequestLogger(logger)(
				otelMiddleware(handler),
			),
		)

		// when: processing request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		// then: should work correctly
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		if rec.Body.String() != "success" {
			t.Errorf("expected body 'success', got %q", rec.Body.String())
		}

		// Span should be created
		spans := spanExporter.GetSpans()
		if len(spans) != 1 {
			t.Errorf("expected 1 span, got %d", len(spans))
		}
	})

	t.Run("handles invalid traceparent gracefully", func(t *testing.T) {
		tests := []struct {
			name        string
			traceparent string
		}{
			{
				name:        "malformed format",
				traceparent: "invalid-format",
			},
			{
				name:        "wrong version",
				traceparent: "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
			{
				name:        "invalid trace ID",
				traceparent: "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
			},
			{
				name:        "invalid span ID",
				traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// given: OTel middleware with invalid traceparent
				spanExporter := tracetest.NewInMemoryExporter()
				tp := sdktrace.NewTracerProvider(
					sdktrace.WithSyncer(spanExporter),
				)

				propagator := propagation.TraceContext{}

				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})

				middleware := mustOTelMiddleware(
					t,
					vital.WithTracerProvider(tp),
					vital.WithPropagator(propagator),
				)
				wrappedHandler := middleware(handler)

				// when: processing request with invalid traceparent
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Traceparent", tt.traceparent)

				rec := httptest.NewRecorder()
				wrappedHandler.ServeHTTP(rec, req)

				// then: should generate new trace (not crash)
				if rec.Code != http.StatusOK {
					t.Errorf("expected status 200, got %d", rec.Code)
				}

				spans := spanExporter.GetSpans()
				if len(spans) != 1 {
					t.Errorf("expected 1 span, got %d", len(spans))
				}

				// Should have valid trace ID (new trace generated)
				span := spans[0]
				if !span.SpanContext.TraceID().IsValid() {
					t.Error("expected valid trace ID for new trace")
				}
			})
		}
	})
}

func findMetricByName(scopeMetrics []metricdata.ScopeMetrics, name string) *metricdata.Metrics {
	for _, sm := range scopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}

	return nil
}

func mustOTelMiddleware(t *testing.T, opts ...vital.OTelOption) vital.Middleware {
	t.Helper()

	middleware, err := vital.OTel(opts...)
	if err != nil {
		t.Fatalf("failed to create OTel middleware: %v", err)
	}

	return middleware
}

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
