package vital

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"go.opentelemetry.io/otel/trace"
)

// ContextKey is a strongly-typed key for storing values in context that should be logged.
type ContextKey struct {
	Name string
}

// Registry manages a collection of context keys to extract and log.
// Each ContextHandler can have its own Registry for isolation.
type Registry struct {
	keys   map[ContextKey]struct{}
	cached []ContextKey
	mutex  sync.RWMutex
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		keys:  make(map[ContextKey]struct{}),
		mutex: sync.RWMutex{},
	}
}

// Register adds a context key to this registry.
func (r *Registry) Register(key ContextKey) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.keys[key] = struct{}{}
	r.cached = nil
}

// Keys returns a copy of all registered keys for safe iteration.
// The internal cache is invalidated when new keys are registered; callers
// may freely mutate the returned slice without affecting future calls.
func (r *Registry) Keys() []ContextKey {
	r.mutex.RLock()

	if r.cached != nil {
		keys := append([]ContextKey(nil), r.cached...)
		r.mutex.RUnlock()

		return keys
	}

	r.mutex.RUnlock()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.cached == nil {
		cached := make([]ContextKey, 0, len(r.keys))
		for key := range r.keys {
			cached = append(cached, key)
		}

		r.cached = cached
	}

	return append([]ContextKey(nil), r.cached...)
}

// ContextHandler is a slog.Handler that automatically extracts registered context values
// and adds them as log attributes.
// When WithBuiltinKeys is used, it also extracts trace_id, span_id, and trace_flags from
// the OTel span context stored in the context by any OTel-compliant middleware.
type ContextHandler struct {
	handler     slog.Handler
	registry    *Registry
	builtinKeys bool
}

// ContextHandlerOption is a functional option for configuring a ContextHandler.
type ContextHandlerOption func(*ContextHandler)

// WithRegistry provides a custom registry for the ContextHandler.
// Use this when you want full control over the registry instance.
func WithRegistry(registry *Registry) ContextHandlerOption {
	return func(h *ContextHandler) {
		h.registry = registry
	}
}

// WithBuiltinKeys enables automatic extraction of trace_id, span_id, and trace_flags
// from the OTel span context. This works with any OTel-compliant middleware (e.g., otelhttp).
func WithBuiltinKeys() ContextHandlerOption {
	return func(h *ContextHandler) {
		h.builtinKeys = true
	}
}

// WithContextKeys registers specific context keys to be extracted and logged.
// This is useful for adding custom application-specific keys.
func WithContextKeys(keys ...ContextKey) ContextHandlerOption {
	return func(h *ContextHandler) {
		for _, key := range keys {
			h.registry.Register(key)
		}
	}
}

// NewContextHandler creates a new ContextHandler wrapping the provided handler.
// If the provided handler is already a ContextHandler, it unwraps it first to avoid nesting.
// Options can be provided to configure which context keys are extracted.
//
// Example usage:
//
//	handler := vital.NewContextHandler(
//	    slog.NewJSONHandler(os.Stdout, nil),
//	    vital.WithBuiltinKeys(),              // Include trace context keys
//	    vital.WithContextKeys(UserIDKey),     // Add custom keys
//	)
func NewContextHandler(handler slog.Handler, opts ...ContextHandlerOption) *ContextHandler {
	// Unwrap nested ContextHandlers to avoid double-wrapping
	if contextHandler, ok := handler.(*ContextHandler); ok {
		handler = contextHandler.handler
	}

	// Create handler with empty registry
	//nolint:varnamelen // h is a conventional short name for handler variables
	h := &ContextHandler{
		handler:  handler,
		registry: NewRegistry(),
	}

	// Apply options
	for _, opt := range opts {
		opt(h)
	}

	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle processes the log record, extracting registered context values and adding them as attributes.
func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.builtinKeys {
		if spanCtx := trace.SpanFromContext(ctx).SpanContext(); spanCtx.IsValid() {
			record.AddAttrs(
				slog.String("trace_id", spanCtx.TraceID().String()),
				slog.String("span_id", spanCtx.SpanID().String()),
				slog.String("trace_flags", spanCtx.TraceFlags().String()),
			)
		}
	}

	// Extract all registered context keys and add them to the log record
	for _, key := range h.registry.Keys() {
		if value := ctx.Value(key); value != nil {
			record.AddAttrs(slog.Attr{
				Key:   key.Name,
				Value: slog.AnyValue(value),
			})
		}
	}

	err := h.handler.Handle(ctx, record)
	if err != nil {
		return fmt.Errorf("failed to handle log record: %w", err)
	}

	return nil
}

// WithAttrs returns a new handler with the given attributes added.
// The returned handler preserves the same registry and builtinKeys setting as the original.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	ch := NewContextHandler(
		h.handler.WithAttrs(attrs),
		WithRegistry(h.registry),
	)
	ch.builtinKeys = h.builtinKeys

	return ch
}

// WithGroup returns a new handler with the given group name.
// The returned handler preserves the same registry and builtinKeys setting as the original.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	ch := NewContextHandler(
		h.handler.WithGroup(name),
		WithRegistry(h.registry),
	)
	ch.builtinKeys = h.builtinKeys

	return ch
}

// Registry returns the handler's registry for inspection.
func (h *ContextHandler) Registry() *Registry {
	return h.registry
}

// Unwrap returns the underlying handler wrapped by this ContextHandler.
func (h *ContextHandler) Unwrap() slog.Handler {
	return h.handler
}

var (
	// ErrInvalidLogLevel is returned when an invalid log level is provided.
	ErrInvalidLogLevel = errors.New("invalid log level")
	// ErrInvalidLogFormat is returned when an invalid log format is provided.
	ErrInvalidLogFormat = errors.New("invalid log format")
)

// LogConfig holds configuration for the logger.
type LogConfig struct {
	// Level is the log level (debug, info, warn, error).
	Level string
	// Format is the log format (json, text).
	Format string
	// AddSource includes the source file and line number in the log.
	AddSource bool
}

// NewHandlerFromConfig creates a new slog.Handler based on the provided configuration.
// Returns an error if level or format are invalid.
func NewHandlerFromConfig(cfg LogConfig, opts ...ContextHandlerOption) (slog.Handler, error) {
	var level slog.Level

	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return nil, fmt.Errorf("%w: %q (must be debug, info, warn, or error)", ErrInvalidLogLevel, cfg.Level)
	}

	//nolint:exhaustruct // ReplaceAttr is optional and not needed for basic configuration
	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler

	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	default:
		return nil, fmt.Errorf("%w: %q (must be text or json)", ErrInvalidLogFormat, cfg.Format)
	}

	return NewContextHandler(handler, opts...), nil
}
