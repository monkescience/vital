package vital_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/monkescience/testastic"
	"github.com/monkescience/vital"
	"go.opentelemetry.io/otel/trace"
)

func TestContextHandler(t *testing.T) {
	t.Parallel()
	t.Run("extracts context values", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with a registered context key
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})

		testKey := vital.ContextKey{Name: "test_key"}
		handler := vital.NewContextHandler(baseHandler, vital.WithContextKeys(testKey))
		logger := slog.New(handler)

		ctx := context.WithValue(context.Background(), testKey, "test_value")

		// when: logging with context
		logger.InfoContext(ctx, "test message")

		// then: the context value should be in the log output
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, "test_value", logEntry["test_key"])

		testastic.DeepEqual[any](t, "test message", logEntry["msg"])
	})

	t.Run("handles multiple context keys", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with multiple registered context keys
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)

		key1 := vital.ContextKey{Name: "key1"}
		key2 := vital.ContextKey{Name: "key2"}
		handler := vital.NewContextHandler(baseHandler, vital.WithContextKeys(key1, key2))
		logger := slog.New(handler)

		ctx := context.Background()
		ctx = context.WithValue(ctx, key1, "value1")
		ctx = context.WithValue(ctx, key2, "value2")

		// when: logging with context
		logger.InfoContext(ctx, "test message")

		// then: all context values should be in the log output
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, "value1", logEntry["key1"])

		testastic.DeepEqual[any](t, "value2", logEntry["key2"])
	})

	t.Run("omits missing context value", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with a registered key but no value in context
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)

		missingKey := vital.ContextKey{Name: "missing_key"}
		handler := vital.NewContextHandler(baseHandler, vital.WithContextKeys(missingKey))
		logger := slog.New(handler)

		// when: logging without the context value
		logger.InfoContext(context.Background(), "test message")

		// then: the missing key should not be in the log
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.MapNotHasKey(t, logEntry, "missing_key")
	})

	t.Run("includes added attributes", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with added attributes
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler)
		logger := slog.New(handler)

		loggerWithAttrs := logger.With(slog.String("attr1", "value1"))

		// when: logging with the modified logger
		loggerWithAttrs.Info("test message")

		// then: the attribute should be in the log output
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, "value1", logEntry["attr1"])
	})

	t.Run("creates groups", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with a group
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler)
		logger := slog.New(handler)

		loggerWithGroup := logger.WithGroup("group1")

		// when: logging with the grouped logger
		loggerWithGroup.Info("test message", slog.String("key", "value"))

		// then: the group should be created in the log output
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		group, ok := logEntry["group1"].(map[string]any)
		testastic.True(t, ok)

		testastic.DeepEqual[any](t, "value", group["key"])
	})

	t.Run("avoids nesting when wrapping context handler", func(t *testing.T) {
		t.Parallel()

		// given: a context handler wrapping another context handler
		baseHandler := slog.NewJSONHandler(&bytes.Buffer{}, nil)
		handler1 := vital.NewContextHandler(baseHandler)

		// when: wrapping the context handler again
		handler2 := vital.NewContextHandler(handler1)

		// then: it should unwrap and use the original base handler
		testastic.True(t, handler2.Unwrap() == baseHandler)
	})

	t.Run("respects log level", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with Warn level
		baseHandler := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		})
		handler := vital.NewContextHandler(baseHandler)

		ctx := context.Background()

		// when: checking if different log levels are enabled

		// then: only Warn and above should be enabled
		testastic.False(t, handler.Enabled(ctx, slog.LevelInfo))

		testastic.True(t, handler.Enabled(ctx, slog.LevelWarn))

		testastic.True(t, handler.Enabled(ctx, slog.LevelError))
	})

	t.Run("handles different value types", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with keys for different value types
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)

		stringKey := vital.ContextKey{Name: "string_val"}
		intKey := vital.ContextKey{Name: "int_val"}
		boolKey := vital.ContextKey{Name: "bool_val"}

		handler := vital.NewContextHandler(baseHandler, vital.WithContextKeys(stringKey, intKey, boolKey))
		logger := slog.New(handler)

		ctx := context.Background()
		ctx = context.WithValue(ctx, stringKey, "hello")
		ctx = context.WithValue(ctx, intKey, 42)
		ctx = context.WithValue(ctx, boolKey, true)

		// when: logging with context containing different value types
		logger.InfoContext(ctx, "test message")

		// then: all values should be correctly logged with their types
		logOutput := buf.String()

		testastic.Contains(t, logOutput, `"string_val":"hello"`)

		testastic.Contains(t, logOutput, `"int_val":42`)

		testastic.Contains(t, logOutput, `"bool_val":true`)
	})
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	t.Run("registers keys", func(t *testing.T) {
		t.Parallel()

		// given: a new registry
		registry := vital.NewRegistry()

		testKey := vital.ContextKey{Name: "test_key"}

		// when: registering a key
		registry.Register(testKey)

		// then: the key should be in the registry
		keys := registry.Keys()
		found := false

		for _, key := range keys {
			if key.Name == testKey.Name {
				found = true

				break
			}
		}

		testastic.True(t, found)
	})

	t.Run("reflects keys registered after first access", func(t *testing.T) {
		t.Parallel()

		// given: a registry with one key already accessed
		registry := vital.NewRegistry()

		key1 := vital.ContextKey{Name: "key1"}
		registry.Register(key1)

		keys := registry.Keys()
		testastic.Len(t, keys, 1)

		// when: registering a new key after Keys() was called
		key2 := vital.ContextKey{Name: "key2"}
		registry.Register(key2)

		// then: subsequent Keys() call should include the new key
		keys = registry.Keys()
		testastic.Len(t, keys, 2)
	})

	t.Run("returns copy so callers cannot mutate the cache", func(t *testing.T) {
		t.Parallel()

		// given: a registry with one key
		registry := vital.NewRegistry()
		registry.Register(vital.ContextKey{Name: "original"})

		// when: the caller mutates the returned slice
		keys := registry.Keys()
		if len(keys) != 1 {
			t.Fatalf("expected 1 key, got %d", len(keys))
		}

		keys[0] = vital.ContextKey{Name: "tampered"}

		// then: the registry's internal cache should be unaffected
		fresh := registry.Keys()
		if len(fresh) != 1 {
			t.Fatalf("expected 1 key after tamper, got %d", len(fresh))
		}

		testastic.Equal(t, "original", fresh[0].Name)
	})

	t.Run("returns all registered keys", func(t *testing.T) {
		t.Parallel()

		// given: a registry with multiple keys
		registry := vital.NewRegistry()

		key1 := vital.ContextKey{Name: "key1"}
		key2 := vital.ContextKey{Name: "key2"}

		registry.Register(key1)
		registry.Register(key2)

		// when: getting all keys
		keys := registry.Keys()

		// then: it should return all registered keys
		testastic.Len(t, keys, 2)

		keyNames := make(map[string]bool)
		for _, key := range keys {
			keyNames[key.Name] = true
		}

		testastic.True(t, keyNames["key1"])
		testastic.True(t, keyNames["key2"])
	})
}

// testSpanContext builds a valid span context using only the otel/trace API,
// avoiding a direct dependency on the OTel SDK.
func testSpanContext(tb testing.TB) (context.Context, trace.SpanContext) {
	tb.Helper()

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	})

	return trace.ContextWithSpanContext(context.Background(), spanCtx), spanCtx
}

func TestContextHandler_WithBuiltinKeys_OTelSpanContext(t *testing.T) {
	t.Parallel()
	t.Run("extracts trace context from OTel span", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with builtin keys and an active OTel span
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler, vital.WithBuiltinKeys())
		logger := slog.New(handler)

		ctx, spanCtx := testSpanContext(t)

		// when: logging with the span context
		logger.InfoContext(ctx, "test message")

		// then: trace_id, span_id, and trace_flags should be in the log output
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, spanCtx.TraceID().String(), logEntry["trace_id"])

		testastic.DeepEqual[any](t, spanCtx.SpanID().String(), logEntry["span_id"])

		testastic.DeepEqual[any](t, spanCtx.TraceFlags().String(), logEntry["trace_flags"])
	})

	t.Run("omits trace context when no span in context", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with builtin keys but no active span
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler, vital.WithBuiltinKeys())
		logger := slog.New(handler)

		// when: logging without a span
		logger.InfoContext(context.Background(), "test message")

		// then: trace fields should not be present
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.MapNotHasKey(t, logEntry, "trace_id")

		testastic.MapNotHasKey(t, logEntry, "span_id")
	})

	t.Run("preserves builtin keys through WithAttrs", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with builtin keys, then WithAttrs applied
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler, vital.WithBuiltinKeys())
		logger := slog.New(handler)
		logger = logger.With(slog.String("service", "test"))

		ctx, spanCtx := testSpanContext(t)

		// when: logging
		logger.InfoContext(ctx, "test message")

		// then: trace context should still be extracted
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, spanCtx.TraceID().String(), logEntry["trace_id"])

		testastic.DeepEqual[any](t, "test", logEntry["service"])
	})

	t.Run("preserves builtin keys through WithGroup", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with builtin keys, then WithGroup applied
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		handler := vital.NewContextHandler(baseHandler, vital.WithBuiltinKeys())
		logger := slog.New(handler)
		loggerWithGroup := logger.WithGroup("group1")

		ctx, spanCtx := testSpanContext(t)

		// when: logging
		loggerWithGroup.InfoContext(ctx, "test message", slog.String("key", "value"))

		// then: trace context should be extracted within the group (slog groups all Handle attrs)
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		group, ok := logEntry["group1"].(map[string]any)
		testastic.True(t, ok)

		testastic.DeepEqual[any](t, spanCtx.TraceID().String(), group["trace_id"])
	})

	t.Run("works alongside custom context keys", func(t *testing.T) {
		t.Parallel()

		// given: a context handler with both builtin keys and custom keys
		var buf bytes.Buffer

		baseHandler := slog.NewJSONHandler(&buf, nil)
		customKey := vital.ContextKey{Name: "request_id"}
		handler := vital.NewContextHandler(baseHandler,
			vital.WithBuiltinKeys(),
			vital.WithContextKeys(customKey),
		)
		logger := slog.New(handler)

		ctx, spanCtx := testSpanContext(t)
		ctx = context.WithValue(ctx, customKey, "req-123")

		// when: logging
		logger.InfoContext(ctx, "test message")

		// then: both OTel trace context and custom keys should be present
		var logEntry map[string]any

		err := json.Unmarshal(buf.Bytes(), &logEntry)
		testastic.NoError(t, err)

		testastic.DeepEqual[any](t, spanCtx.TraceID().String(), logEntry["trace_id"])

		testastic.DeepEqual[any](t, "req-123", logEntry["request_id"])
	})
}

func TestNewHandlerFromConfig(t *testing.T) {
	t.Parallel()
	t.Run("returns error with empty log level", func(t *testing.T) {
		t.Parallel()

		// given: a config with empty level
		cfg := vital.LogConfig{
			Level:  "",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		testastic.Error(t, err)

		testastic.Nil(t, handler)
	})

	t.Run("returns error with empty format", func(t *testing.T) {
		t.Parallel()

		// given: a config with empty format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		testastic.Error(t, err)

		testastic.Nil(t, handler)
	})

	t.Run("creates handler with debug level", func(t *testing.T) {
		t.Parallel()

		// given: a config with debug level
		cfg := vital.LogConfig{
			Level:  "debug",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and debug level should be enabled
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testastic.True(t, handler.Enabled(context.Background(), slog.LevelDebug))
	})

	t.Run("creates handler with info level", func(t *testing.T) {
		t.Parallel()

		// given: a config with info level
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and info level should be enabled but debug should not
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testastic.True(t, handler.Enabled(context.Background(), slog.LevelInfo))

		testastic.False(t, handler.Enabled(context.Background(), slog.LevelDebug))
	})

	t.Run("creates handler with warn level", func(t *testing.T) {
		t.Parallel()

		// given: a config with warn level
		cfg := vital.LogConfig{
			Level:  "warn",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and warn and error should be enabled, info and debug should not
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testastic.True(t, handler.Enabled(context.Background(), slog.LevelWarn))

		testastic.True(t, handler.Enabled(context.Background(), slog.LevelError))

		testastic.False(t, handler.Enabled(context.Background(), slog.LevelInfo))
	})

	t.Run("creates handler with error level", func(t *testing.T) {
		t.Parallel()

		// given: a config with error level
		cfg := vital.LogConfig{
			Level:  "error",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and only error level should be enabled
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testastic.True(t, handler.Enabled(context.Background(), slog.LevelError))

		testastic.False(t, handler.Enabled(context.Background(), slog.LevelWarn))
	})

	t.Run("returns error with invalid log level", func(t *testing.T) {
		t.Parallel()

		// given: a config with invalid level
		cfg := vital.LogConfig{
			Level:  "invalid",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		testastic.Error(t, err)

		testastic.Nil(t, handler)
	})

	t.Run("creates handler with JSON format", func(t *testing.T) {
		t.Parallel()

		// given: a config with JSON format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a ContextHandler
		testastic.NoError(t, err)

		_, ok := handler.(*vital.ContextHandler)
		testastic.True(t, ok)
	})

	t.Run("creates handler with text format", func(t *testing.T) {
		t.Parallel()

		// given: a config with text format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "text",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a ContextHandler
		testastic.NoError(t, err)

		_, ok := handler.(*vital.ContextHandler)
		testastic.True(t, ok)
	})

	t.Run("returns error with invalid format", func(t *testing.T) {
		t.Parallel()

		// given: a config with invalid format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "invalid",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		testastic.Error(t, err)

		testastic.Nil(t, handler)
	})

	t.Run("creates handler with AddSource enabled", func(t *testing.T) {
		t.Parallel()

		// given: a config with AddSource enabled
		cfg := vital.LogConfig{
			Level:     "info",
			Format:    "json",
			AddSource: true,
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a valid handler
		testastic.NoError(t, err)

		_, ok := handler.(*vital.ContextHandler)
		testastic.True(t, ok)
	})

	t.Run("creates handler with context handler options", func(t *testing.T) {
		t.Parallel()

		// given: a config and context handler options
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		testKey := vital.ContextKey{Name: "custom_key"}

		// when: creating a handler with custom options
		handler, err := vital.NewHandlerFromConfig(cfg, vital.WithContextKeys(testKey))
		// then: it should succeed and include the custom context keys
		testastic.NoError(t, err)

		contextHandler, ok := handler.(*vital.ContextHandler)
		if !ok {
			t.Fatal("expected handler to be a ContextHandler")
		}

		// Verify the key is registered
		keys := contextHandler.Registry().Keys()
		found := false

		for _, key := range keys {
			if key.Name == testKey.Name {
				found = true

				break
			}
		}

		testastic.True(t, found)
	})

	t.Run("creates handler with builtin keys option", func(t *testing.T) {
		t.Parallel()

		// given: a config with builtin keys option
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		// when: creating a handler with builtin keys
		handler, err := vital.NewHandlerFromConfig(cfg, vital.WithBuiltinKeys())
		// then: it should succeed and have builtin keys enabled
		testastic.NoError(t, err)

		_, ok := handler.(*vital.ContextHandler)
		testastic.True(t, ok)
	})
}

func BenchmarkRegistryKeys(b *testing.B) {
	registry := vital.NewRegistry()

	key1 := vital.ContextKey{Name: "key1"}
	key2 := vital.ContextKey{Name: "key2"}
	key3 := vital.ContextKey{Name: "key3"}

	registry.Register(key1)
	registry.Register(key2)
	registry.Register(key3)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = registry.Keys()
	}
}

func BenchmarkContextHandlerHandle(b *testing.B) {
	var buf bytes.Buffer

	baseHandler := slog.NewJSONHandler(&buf, nil)
	handler := vital.NewContextHandler(baseHandler, vital.WithBuiltinKeys())
	logger := slog.New(handler)

	ctx, _ := testSpanContext(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		buf.Reset()
		logger.InfoContext(ctx, "benchmark")
	}
}

// ExampleNewContextHandler demonstrates logging registered context values.
func ExampleNewContextHandler() {
	requestIDKey := vital.ContextKey{Name: "request_id"}

	handler := vital.NewContextHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.Attr{}
				}

				return a
			},
		}),
		vital.WithContextKeys(requestIDKey),
	)

	logger := slog.New(handler)
	ctx := context.WithValue(context.Background(), requestIDKey, "abc-123")
	logger.InfoContext(ctx, "handling request")

	fmt.Println("logged with request_id from context")

	// Output:
	// level=INFO msg="handling request" request_id=abc-123
	// logged with request_id from context
}
