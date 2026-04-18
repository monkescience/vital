package vital

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter

	statusCode   int
	wroteHeader  bool
	bytesWritten int64
	hijacked     bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	if rw, ok := w.(*responseWriter); ok {
		return rw
	}

	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code and calls the underlying WriteHeader.
func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(data []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	written, err := rw.ResponseWriter.Write(data)
	rw.bytesWritten += int64(written)

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context.
	return written, err
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// FlushError writes a default status header if none was written, then delegates
// to http.ResponseController.Flush so the Unwrap chain is walked. Returns
// http.ErrNotSupported when nothing in the chain can flush.
func (rw *responseWriter) FlushError() error {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context.
	return http.NewResponseController(rw.ResponseWriter).Flush()
}

func (rw *responseWriter) Flush() {
	_ = rw.FlushError()
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(rw.ResponseWriter).Hijack()
	if err != nil {
		//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context.
		return nil, nil, err
	}

	rw.hijacked = true

	return conn, brw, nil
}

func (rw *responseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := rw.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context
	return pusher.Push(target, opts)
}

func (rw *responseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	readerFrom, ok := rw.ResponseWriter.(io.ReaderFrom)
	if ok {
		written, err := readerFrom.ReadFrom(reader)
		rw.bytesWritten += written

		//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context.
		return written, err
	}

	written, err := io.Copy(rw.ResponseWriter, reader)
	rw.bytesWritten += written

	//nolint:wrapcheck // Delegating to underlying ResponseWriter, wrapping would lose context.
	return written, err
}

func (rw *responseWriter) responseStarted() bool {
	return rw.wroteHeader || rw.bytesWritten > 0 || rw.hijacked
}
