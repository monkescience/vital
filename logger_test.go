package vital_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/monkescience/vital"
)

func TestContextHandler(t *testing.T) {
	t.Run("extracts context values", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("failed to parse log output: %v", err)
		}

		if logEntry["test_key"] != "test_value" {
			t.Errorf("expected test_key='test_value', got %v", logEntry["test_key"])
		}

		if logEntry["msg"] != "test message" {
			t.Errorf("expected msg='test message', got %v", logEntry["msg"])
		}
	})

	t.Run("handles multiple context keys", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("failed to parse log output: %v", err)
		}

		if logEntry["key1"] != "value1" {
			t.Errorf("expected key1='value1', got %v", logEntry["key1"])
		}

		if logEntry["key2"] != "value2" {
			t.Errorf("expected key2='value2', got %v", logEntry["key2"])
		}
	})

	t.Run("omits missing context value", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("failed to parse log output: %v", err)
		}

		if _, exists := logEntry["missing_key"]; exists {
			t.Error("expected missing_key to not be in log output")
		}
	})

	t.Run("includes added attributes", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("failed to parse log output: %v", err)
		}

		if logEntry["attr1"] != "value1" {
			t.Errorf("expected attr1='value1', got %v", logEntry["attr1"])
		}
	})

	t.Run("creates groups", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("failed to parse log output: %v", err)
		}

		group, ok := logEntry["group1"].(map[string]any)
		if !ok {
			t.Fatal("expected group1 to be a map")
		}

		if group["key"] != "value" {
			t.Errorf("expected group1.key='value', got %v", group["key"])
		}
	})

	t.Run("avoids nesting when wrapping context handler", func(t *testing.T) {
		// given: a context handler wrapping another context handler
		baseHandler := slog.NewJSONHandler(&bytes.Buffer{}, nil)
		handler1 := vital.NewContextHandler(baseHandler)

		// when: wrapping the context handler again
		handler2 := vital.NewContextHandler(handler1)

		// then: it should unwrap and use the original base handler
		if handler2.Unwrap() != baseHandler {
			t.Error("expected handler2 to unwrap handler1 and use the base handler")
		}
	})

	t.Run("respects log level", func(t *testing.T) {
		// given: a context handler with Warn level
		baseHandler := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		})
		handler := vital.NewContextHandler(baseHandler)

		ctx := context.Background()

		// when: checking if different log levels are enabled

		// then: only Warn and above should be enabled
		if handler.Enabled(ctx, slog.LevelInfo) {
			t.Error("expected LevelInfo to be disabled when handler level is Warn")
		}

		if !handler.Enabled(ctx, slog.LevelWarn) {
			t.Error("expected LevelWarn to be enabled")
		}

		if !handler.Enabled(ctx, slog.LevelError) {
			t.Error("expected LevelError to be enabled")
		}
	})

	t.Run("handles different value types", func(t *testing.T) {
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

		if !strings.Contains(logOutput, `"string_val":"hello"`) {
			t.Error("expected string_val to be in log output")
		}

		if !strings.Contains(logOutput, `"int_val":42`) {
			t.Error("expected int_val to be in log output")
		}

		if !strings.Contains(logOutput, `"bool_val":true`) {
			t.Error("expected bool_val to be in log output")
		}
	})
}

func TestRegistry(t *testing.T) {
	t.Run("registers keys", func(t *testing.T) {
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

		if !found {
			t.Error("expected test_key to be registered")
		}
	})

	t.Run("returns all registered keys", func(t *testing.T) {
		// given: a registry with multiple keys
		registry := vital.NewRegistry()

		key1 := vital.ContextKey{Name: "key1"}
		key2 := vital.ContextKey{Name: "key2"}

		registry.Register(key1)
		registry.Register(key2)

		// when: getting all keys
		keys := registry.Keys()

		// then: it should return all registered keys
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}

		keyNames := make(map[string]bool)
		for _, key := range keys {
			keyNames[key.Name] = true
		}

		if !keyNames["key1"] || !keyNames["key2"] {
			t.Error("expected both key1 and key2 to be in registry")
		}
	})
}

func TestBuiltinKeys(t *testing.T) {
	t.Run("includes all trace context keys", func(t *testing.T) {
		// when: getting builtin keys
		keys := vital.BuiltinKeys()

		// then: all trace context keys should be included
		expectedKeys := map[string]bool{
			"trace_id":    false,
			"span_id":     false,
			"trace_flags": false,
		}

		for _, key := range keys {
			if _, exists := expectedKeys[key.Name]; exists {
				expectedKeys[key.Name] = true
			}
		}

		for keyName, found := range expectedKeys {
			if !found {
				t.Errorf("expected %s to be in builtin keys", keyName)
			}
		}
	})
}

func TestNewHandlerFromConfig(t *testing.T) {
	t.Run("returns error with empty log level", func(t *testing.T) {
		// given: a config with empty level
		cfg := vital.LogConfig{
			Level:  "",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		if err == nil {
			t.Error("expected error for empty log level")
		}

		if handler != nil {
			t.Error("expected nil handler when error occurs")
		}
	})

	t.Run("returns error with empty format", func(t *testing.T) {
		// given: a config with empty format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		if err == nil {
			t.Error("expected error for empty format")
		}

		if handler != nil {
			t.Error("expected nil handler when error occurs")
		}
	})

	t.Run("creates handler with debug level", func(t *testing.T) {
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

		if !handler.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug level to be enabled")
		}
	})

	t.Run("creates handler with info level", func(t *testing.T) {
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

		if !handler.Enabled(context.Background(), slog.LevelInfo) {
			t.Error("expected info level to be enabled")
		}

		if handler.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug level to be disabled")
		}
	})

	t.Run("creates handler with warn level", func(t *testing.T) {
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

		if !handler.Enabled(context.Background(), slog.LevelWarn) {
			t.Error("expected warn level to be enabled")
		}

		if !handler.Enabled(context.Background(), slog.LevelError) {
			t.Error("expected error level to be enabled")
		}

		if handler.Enabled(context.Background(), slog.LevelInfo) {
			t.Error("expected info level to be disabled")
		}
	})

	t.Run("creates handler with error level", func(t *testing.T) {
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

		if !handler.Enabled(context.Background(), slog.LevelError) {
			t.Error("expected error level to be enabled")
		}

		if handler.Enabled(context.Background(), slog.LevelWarn) {
			t.Error("expected warn level to be disabled")
		}
	})

	t.Run("returns error with invalid log level", func(t *testing.T) {
		// given: a config with invalid level
		cfg := vital.LogConfig{
			Level:  "invalid",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		if err == nil {
			t.Error("expected error for invalid log level")
		}

		if handler != nil {
			t.Error("expected nil handler when error occurs")
		}
	})

	t.Run("creates handler with JSON format", func(t *testing.T) {
		// given: a config with JSON format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a ContextHandler
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, ok := handler.(*vital.ContextHandler)
		if !ok {
			t.Error("expected handler to be a ContextHandler")
		}
	})

	t.Run("creates handler with text format", func(t *testing.T) {
		// given: a config with text format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "text",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a ContextHandler
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, ok := handler.(*vital.ContextHandler)
		if !ok {
			t.Error("expected handler to be a ContextHandler")
		}
	})

	t.Run("returns error with invalid format", func(t *testing.T) {
		// given: a config with invalid format
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "invalid",
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)

		// then: it should return an error
		if err == nil {
			t.Error("expected error for invalid format")
		}

		if handler != nil {
			t.Error("expected nil handler when error occurs")
		}
	})

	t.Run("creates handler with AddSource enabled", func(t *testing.T) {
		// given: a config with AddSource enabled
		cfg := vital.LogConfig{
			Level:     "info",
			Format:    "json",
			AddSource: true,
		}

		// when: creating a handler from config
		handler, err := vital.NewHandlerFromConfig(cfg)
		// then: it should succeed and create a valid handler
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, ok := handler.(*vital.ContextHandler)
		if !ok {
			t.Error("expected handler to be a ContextHandler")
		}
	})

	t.Run("creates handler with context handler options", func(t *testing.T) {
		// given: a config and context handler options
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		testKey := vital.ContextKey{Name: "custom_key"}

		// when: creating a handler with custom options
		handler, err := vital.NewHandlerFromConfig(cfg, vital.WithContextKeys(testKey))
		// then: it should succeed and include the custom context keys
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

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

		if !found {
			t.Error("expected custom_key to be registered")
		}
	})

	t.Run("creates handler with builtin keys option", func(t *testing.T) {
		// given: a config with builtin keys option
		cfg := vital.LogConfig{
			Level:  "info",
			Format: "json",
		}

		// when: creating a handler with builtin keys
		handler, err := vital.NewHandlerFromConfig(cfg, vital.WithBuiltinKeys())
		// then: it should succeed and include builtin keys
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		contextHandler, ok := handler.(*vital.ContextHandler)
		if !ok {
			t.Fatal("expected handler to be a ContextHandler")
		}

		// Verify builtin keys are registered
		keys := contextHandler.Registry().Keys()
		expectedKeys := map[string]bool{
			"trace_id":    false,
			"span_id":     false,
			"trace_flags": false,
		}

		for _, key := range keys {
			if _, exists := expectedKeys[key.Name]; exists {
				expectedKeys[key.Name] = true
			}
		}

		for keyName, found := range expectedKeys {
			if !found {
				t.Errorf("expected builtin key %s to be registered", keyName)
			}
		}
	})
}
