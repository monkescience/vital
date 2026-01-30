package vital

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

const defaultMaxBodySize = 1024 * 1024 // 1MB

var (
	// ErrEmptyBody is returned when the request body is empty.
	ErrEmptyBody = errors.New("empty request body")
	// ErrBodyTooLarge is returned when the request body exceeds the maximum size.
	ErrBodyTooLarge = errors.New("request body exceeds maximum size")
	// ErrMissingFields is returned when required fields are missing from the request.
	ErrMissingFields = errors.New("missing required fields")
)

// DecodeOption configures body decoding behavior.
type DecodeOption func(*decodeConfig)

type decodeConfig struct {
	maxBodySize int64
}

// WithMaxBodySize sets a custom body size limit.
func WithMaxBodySize(size int64) DecodeOption {
	return func(c *decodeConfig) {
		c.maxBodySize = size
	}
}

// DecodeJSON decodes a JSON request body into type T with validation.
func DecodeJSON[T any](r *http.Request, opts ...DecodeOption) (T, error) {
	var zero T

	config := decodeConfig{
		maxBodySize: defaultMaxBodySize,
	}

	for _, opt := range opts {
		opt(&config)
	}

	limitedReader := io.LimitReader(r.Body, config.maxBodySize+1)
	decoder := json.NewDecoder(limitedReader)

	var result T

	err := decoder.Decode(&result)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return zero, ErrEmptyBody
		}

		if decoder.More() {
			var buf [1]byte

			_, readErr := limitedReader.Read(buf[:])
			if readErr == nil {
				return zero, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, config.maxBodySize)
			}
		}

		return zero, fmt.Errorf("invalid JSON: %w", err)
	}

	var buf [1]byte

	n, _ := limitedReader.Read(buf[:])
	if n > 0 {
		return zero, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, config.maxBodySize)
	}

	err = validateRequired(result)
	if err != nil {
		return zero, err
	}

	return result, nil
}

// DecodeForm decodes a form urlencoded request body into type T with validation.
func DecodeForm[T any](r *http.Request, opts ...DecodeOption) (T, error) {
	var zero T

	config := decodeConfig{
		maxBodySize: defaultMaxBodySize,
	}

	for _, opt := range opts {
		opt(&config)
	}

	r.Body = http.MaxBytesReader(nil, r.Body, config.maxBodySize)

	err := r.ParseForm()
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return zero, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, config.maxBodySize)
		}

		return zero, fmt.Errorf("invalid form data: %w", err)
	}

	var result T

	err = decodeFormToStruct(r.Form, &result)
	if err != nil {
		return zero, err
	}

	err = validateRequired(result)
	if err != nil {
		return zero, err
	}

	return result, nil
}

func decodeFormToStruct(form map[string][]string, target any) error {
	val := reflect.ValueOf(target).Elem()
	typ := val.Type()

	for i := range val.NumField() {
		field := val.Field(i)
		fieldType := typ.Field(i)

		if !field.CanSet() {
			continue
		}

		formTag := fieldType.Tag.Get("form")
		if formTag == "" {
			formTag = strings.ToLower(fieldType.Name)
		}

		formValues, exists := form[formTag]
		if !exists || len(formValues) == 0 {
			continue
		}

		formValue := formValues[0]

		//nolint:exhaustive // Only handling supported field types for form decoding
		switch field.Kind() {
		case reflect.String:
			field.SetString(formValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intVal, err := strconv.ParseInt(formValue, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid integer value for field %s: %w", fieldType.Name, err)
			}

			field.SetInt(intVal)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintVal, err := strconv.ParseUint(formValue, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid unsigned integer value for field %s: %w", fieldType.Name, err)
			}

			field.SetUint(uintVal)
		case reflect.Float32, reflect.Float64:
			floatVal, err := strconv.ParseFloat(formValue, 64)
			if err != nil {
				return fmt.Errorf("invalid float value for field %s: %w", fieldType.Name, err)
			}

			field.SetFloat(floatVal)
		case reflect.Bool:
			boolVal, err := strconv.ParseBool(formValue)
			if err != nil {
				return fmt.Errorf("invalid boolean value for field %s: %w", fieldType.Name, err)
			}

			field.SetBool(boolVal)
		default:
			// Unsupported field types are silently skipped
		}
	}

	return nil
}

func validateRequired(v any) error {
	val := reflect.ValueOf(v)
	typ := val.Type()

	var missingFields []string

	for i := range val.NumField() {
		field := val.Field(i)
		fieldType := typ.Field(i)

		requiredTag := fieldType.Tag.Get("required")
		if requiredTag != "true" {
			continue
		}

		if isZeroValue(field) {
			fieldName := getFieldName(fieldType)
			missingFields = append(missingFields, fieldName)
		}
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingFields, strings.Join(missingFields, ", "))
	}

	return nil
}

func isZeroValue(v reflect.Value) bool {
	//nolint:exhaustive // Only handling common field types for zero value check
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

func getFieldName(field reflect.StructField) string {
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" && parts[0] != "-" {
			return parts[0]
		}
	}

	if formTag := field.Tag.Get("form"); formTag != "" {
		return formTag
	}

	return strings.ToLower(field.Name)
}
