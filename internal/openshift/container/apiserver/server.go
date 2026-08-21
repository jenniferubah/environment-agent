// Package apiserver provides the HTTP server and middleware for the container API.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/container/httperror"
	oapigen "github.com/dcm-project/environment-agent/internal/openshift/container/oapi/server"
	"github.com/dcm-project/environment-agent/internal/openshift/container/util"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-chi/chi/v5"
)

// Server is the HTTP server for the container service provider API.
type Server struct {
	cfg     *config.Config
	logger  *slog.Logger
	srv     *http.Server
	onReady func(context.Context)
}

// newBadRequestHandler returns a handler that writes a 400 Bad Request
// response with an RFC 9457 application/problem+json body. It is used
// by the parameter binding layer (generated chi wrapper) and OpenAPI
// validation middleware.
func newBadRequestHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		detail := scrubValidationError(err)
		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, httperror.InvalidArgumentTitle, detail, requestInstance(r))
	}
}

// NewRequestErrorHandler returns an error handler for the strict adapter's
// RequestErrorHandlerFunc that writes an RFC 9457 INVALID_ARGUMENT response.
func NewRequestErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return newBadRequestHandler(logger)
}

// NewResponseErrorHandler returns an error handler for the strict adapter's
// ResponseErrorHandlerFunc that writes an RFC 9457 INTERNAL response without
// exposing implementation details.
func NewResponseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("strict handler response error", "error", err)
		httperror.WriteResponse(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, httperror.InternalTitle, httperror.InternalDetail, requestInstance(r))
	}
}

// requestInstance returns a pointer to the request URI (path + query string)
// for use as the RFC 9457 instance field. Returns nil if the request is nil.
func requestInstance(r *http.Request) *string {
	if r == nil {
		return nil
	}
	return util.Ptr(r.URL.RequestURI())
}

// readinessProbeTimeout is how long to wait for the server to confirm it is
// serving HTTP requests before giving up and skipping the onReady callback.
const readinessProbeTimeout = 5 * time.Second

// readinessProbeInterval is the polling interval for the self-probe that
// checks the health endpoint before firing onReady.
const readinessProbeInterval = 50 * time.Millisecond

// WithOnReady registers a callback invoked once the server is confirmed to be
// serving HTTP requests. The server verifies readiness by polling its own
// health endpoint before calling fn. Use this to trigger work (e.g.
// registration) that must wait until the HTTP server is ready.
func (s *Server) WithOnReady(fn func(context.Context)) *Server {
	s.onReady = fn
	return s
}

// scrubValidationError extracts a human-readable constraint message from
// kin-openapi validation errors, stripping raw schema JSON and value dumps.
// For unrecognised error types it returns a generic message to avoid leaking
// internal details to clients.
func scrubValidationError(err error) string {
	const genericMsg = "invalid request"

	// kin-openapi request validation errors carry structured metadata.
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		// Build location prefix (e.g., `parameter "max_page_size" in query`).
		var prefix string
		if p := reqErr.Parameter; p != nil {
			prefix = fmt.Sprintf("parameter %q in %s", p.Name, p.In)
		} else if reqErr.RequestBody != nil {
			prefix = "request body"
		}

		// Extract the human-readable reason from the underlying SchemaError.
		var schemaErr *openapi3.SchemaError
		if errors.As(reqErr.Err, &schemaErr) && schemaErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + schemaErr.Reason
			}
			return schemaErr.Reason
		}

		// Fallback to RequestError.Reason if no SchemaError is available.
		if reqErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + reqErr.Reason
			}
			return reqErr.Reason
		}

		return genericMsg
	}

	// oapi-codegen parameter binding errors — expose the parameter name
	// but strip the raw parse error (e.g. strconv internals).
	var paramErr *oapigen.InvalidParamFormatError
	if errors.As(err, &paramErr) {
		return fmt.Sprintf("invalid format for parameter %q", paramErr.ParamName)
	}

	var reqParamErr *oapigen.RequiredParamError
	if errors.As(err, &reqParamErr) {
		return fmt.Sprintf("missing required parameter %q", reqParamErr.ParamName)
	}

	var reqHeaderErr *oapigen.RequiredHeaderError
	if errors.As(err, &reqHeaderErr) {
		return fmt.Sprintf("missing required header %q", reqHeaderErr.ParamName)
	}

	var cookieErr *oapigen.UnescapedCookieParamError
	if errors.As(err, &cookieErr) {
		return fmt.Sprintf("invalid cookie parameter %q", cookieErr.ParamName)
	}

	var unmarshalErr *oapigen.UnmarshalingParamError
	if errors.As(err, &unmarshalErr) {
		return fmt.Sprintf("invalid value for parameter %q", unmarshalErr.ParamName)
	}

	var tooManyErr *oapigen.TooManyValuesForParamError
	if errors.As(err, &tooManyErr) {
		return fmt.Sprintf("too many values for parameter %q", tooManyErr.ParamName)
	}

	// Unknown error type — return a generic message to avoid leaking internals.
	return genericMsg
}

// statusRecordingResponseWriter wraps an http.ResponseWriter to track
// whether headers have already been sent to the client and the response
// status code. The recovery middleware uses wroteHeader to avoid writing
// a second status line; the logging middleware reads statusCode.
type statusRecordingResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	statusCode  int
}

func (w *statusRecordingResponseWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecordingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
	}
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *statusRecordingResponseWriter) Flush() {
	w.wroteHeader = true
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter, enabling Go 1.20+
// http.ResponseController to discover optional interfaces.
func (w *statusRecordingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// recoveryMiddleware catches panics and returns an RFC 9457
// application/problem+json response instead of a plain-text stack trace.
//
// Special cases:
//   - http.ErrAbortHandler is re-panicked so net/http aborts the connection.
//   - If the handler already called WriteHeader/Write, the middleware logs the
//     panic but does not attempt to write a response (headers already on the wire).
func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusRecordingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				if rec := recover(); rec != nil {
					// http.ErrAbortHandler is a sentinel that tells
					// net/http to abort the connection. Re-panic so
					// the server's built-in handler takes over.
					if rec == http.ErrAbortHandler {
						panic(http.ErrAbortHandler)
					}

					logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))

					if sw.wroteHeader {
						logger.Warn("headers already sent, cannot write RFC 9457 response")
						return
					}

					httperror.WriteResponse(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, httperror.InternalTitle, httperror.InternalDetail, requestInstance(r))
				}
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

// requestTimeoutMiddleware cancels the request context after the configured
// timeout. A zero timeout disables the middleware.
func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if timeout <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requestLoggingMiddleware logs each HTTP request at INFO level with method,
// path, response status code, and duration.
func requestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusRecordingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.statusCode,
				"duration", time.Since(start).String(),
			)
		})
	}
}

// waitForReady polls the server's health endpoint until it returns HTTP 200
// or the context/timeout expires.
func (s *Server) waitForReady(ctx context.Context, addr string) error {
	url := fmt.Sprintf("http://%s/api/v1alpha1/containers/health", addr)
	client := &http.Client{Timeout: 1 * time.Second}

	deadline := time.NewTimer(readinessProbeTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(readinessProbeInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return fmt.Errorf("creating readiness probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("server readiness probe timed out after %s", readinessProbeTimeout)
		case <-ticker.C:
		}
	}
}

// New creates a new Server with the given config and logger.
func New(cfg *config.Config, logger *slog.Logger, handler oapigen.ServerInterface) *Server {
	badReq := newBadRequestHandler(logger)

	r := chi.NewRouter()
	r.Use(recoveryMiddleware(logger))
	r.Use(requestLoggingMiddleware(logger))
	r.Use(requestTimeoutMiddleware(cfg.Server.RequestTimeout))

	// Load OpenAPI spec for request validation middleware.
	spec, err := v1alpha1.GetSpec()
	if err != nil {
		logger.Warn("failed to load OpenAPI spec, request validation disabled", "error", err)
	} else {
		spec.Servers = nil // Avoid URL prefix matching issues.
		specRouter, routerErr := legacyrouter.NewRouter(spec)
		if routerErr != nil {
			logger.Warn("failed to create OpenAPI router, request validation disabled", "error", routerErr)
		} else {
			r.Use(openAPIValidationMiddleware(specRouter, badReq))
		}
	}

	// Reject trailing-slash requests with empty container_id before the
	// generated router sees them. Chi treats /containers/ as a distinct
	// path from /containers/{container_id}, so without this route it would 404.
	emptyIDHandler := func(w http.ResponseWriter, r *http.Request) {
		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, httperror.InvalidArgumentTitle, "container_id is required and cannot be empty", requestInstance(r))
	}
	postPath, pathErr := v1alpha1.PostPath()
	if pathErr != nil {
		logger.Warn("failed to resolve POST path from OpenAPI spec, trailing-slash guards disabled", "error", pathErr)
	} else {
		r.Get(postPath+"/", emptyIDHandler)
		r.Delete(postPath+"/", emptyIDHandler)
	}

	httpHandler := oapigen.HandlerWithOptions(handler, oapigen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: badReq,
	})

	s := &Server{
		cfg:    cfg,
		logger: logger,
		srv: &http.Server{
			Handler:      httpHandler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
	}
	return s
}

// openAPIValidationMiddleware validates incoming requests against the OpenAPI spec.
// It checks path/query parameter constraints and request body validation.
// Routes not found in the spec are passed through to the chi router.
func openAPIValidationMiddleware(specRouter routers.Router, badReq func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := specRouter.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
			}

			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				badReq(w, r, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Run starts the HTTP server on the provided listener and blocks until
// the context is cancelled. Signal handling is the caller's responsibility;
// pass a context that is cancelled on SIGTERM/SIGINT (e.g., via
// signal.NotifyContext).
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	s.logger.Info("server starting", "address", ln.Addr().String())

	serveCh := make(chan error, 1)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveCh <- err
		}
		close(serveCh)
	}()

	if s.onReady != nil {
		if err := s.waitForReady(ctx, ln.Addr().String()); err != nil {
			s.logger.Error("readiness probe failed, skipping onReady callback", "error", err)
		} else {
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("onReady callback panicked", "panic", r)
					}
				}()
				s.onReady(ctx)
			}()
		}
	}

	select {
	case <-ctx.Done():
	case err := <-serveCh:
		if err != nil {
			return fmt.Errorf("serving on %s: %w", ln.Addr(), err)
		}
	}

	s.logger.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}
	return nil
}
