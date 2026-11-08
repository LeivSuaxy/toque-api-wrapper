package trace

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

type Store interface {
	InsertAPIRequest(ctx context.Context, request store.APIRequest) error
}

type Middleware struct {
	store      Store
	logger     *slog.Logger
	trustProxy bool
}

func New(store Store, logger *slog.Logger, trustProxy bool) *Middleware {
	return &Middleware{
		store:      store,
		logger:     logger,
		trustProxy: trustProxy,
	}
}

func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.KeyFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		recorder := &responseRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		var counter *countingReader
		requestBytes := r.ContentLength
		if requestBytes < 0 {
			requestBytes = 0
		}
		if r.Body != nil && r.Body != http.NoBody {
			counter = &countingReader{reader: r.Body}
			r.Body = counter
		}

		next.ServeHTTP(recorder, r)

		if counter != nil && requestBytes == 0 {
			requestBytes = counter.bytes
		}

		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()

		err := m.store.InsertAPIRequest(ctx, store.APIRequest{
			APIKeyID:      key.ID,
			Method:        r.Method,
			Path:          r.URL.Path,
			Query:         r.URL.RawQuery,
			StatusCode:    recorder.status,
			RequestBytes:  requestBytes,
			ResponseBytes: recorder.bytes,
			DurationMS:    time.Since(start).Milliseconds(),
			RemoteIP:      httpx.ClientIP(r, m.trustProxy),
			UserAgent:     r.UserAgent(),
		})
		if err != nil {
			m.logger.Error("record api request", "err", err, "api_key_id", key.ID)
		}
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(data)
	r.bytes += int64(written)
	return written, err
}

func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countingReader) Read(data []byte) (int, error) {
	read, err := r.reader.Read(data)
	r.bytes += int64(read)
	return read, err
}

func (r *countingReader) Close() error {
	if closer, ok := r.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
