package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/PavelAgarkov/service-pkg/logger"
	logger "github.com/PavelAgarkov/service-pkg/logger/zap_engine"
	"github.com/PavelAgarkov/service-pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/rs/xid"
	"go.uber.org/zap"
)

type HTTPServerChi struct {
	port   string
	Router *chi.Mux
	logger *zap.Logger
}

// CreateHTTPChiServer создаёт и запускает HTTP-сервер на chi.
func CreateHTTPChiServer(
	routes func(*HTTPServerChi),
	port string,
	mwf ...func(http.Handler) http.Handler,
) func() {
	s := newHTTPServer(port)

	if len(mwf) > 0 {
		s.Router.Use(mwf...)
	}

	s.apply(routes)

	return s.run(nil)
}

func newHTTPServer(port string) *HTTPServerChi {
	return &HTTPServerChi{
		port:   port,
		Router: chi.NewRouter(),
	}
}

func (s *HTTPServerChi) apply(cfg func(*HTTPServerChi)) {
	cfg(s)
}

func (s *HTTPServerChi) run(balancer http.Handler) func() {
	srv := &http.Server{
		Addr:    s.port,
		Handler: ifNil(balancer, s.Router),
	}

	ctx, cancel := context.WithCancel(context.Background())
	utils.GoRecover(ctx, func(ctx context.Context) {
		defer cancel()
		logger.WriteInfoLog(ctx, &logger_wrapper.LogEntry{
			Msg:       fmt.Sprintf("HTTP server listening on %s", s.port),
			Component: "HTTPServer",
			Method:    "run",
			Args:      s.port,
		})
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(fmt.Sprintf("server stopped: %s", err))
		}
	})

	return s.shutdown(srv)
}

func (s *HTTPServerChi) shutdown(srv *http.Server) func() {
	return func() {
		logger.WriteInfoLog(context.Background(), &logger_wrapper.LogEntry{
			Msg:       "Shutting down HTTP server...",
			Component: "HTTPServer",
			Method:    "shutdown",
			Args:      s.port,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			logger.WriteErrorLog(context.Background(), &logger_wrapper.LogEntry{
				Msg:       "HTTP shutdown failed",
				Error:     err,
				Component: "HTTPServer",
				Method:    "shutdown",
				Args:      s.port,
			})
		}
	}
}

func ifNil(balancer, router http.Handler) http.Handler {
	if balancer != nil {
		return balancer
	}
	return router
}

type contextChiKey string

const correlationChiIDCtxKey contextChiKey = "correlation_id"

func LoggerChiContextMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RecoverChiMiddleware ловит panic внутри хэндлеров.
func RecoverChiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func(c context.Context) {
			if rec := recover(); rec != nil {
				logger.WriteErrorLog(c, &logger_wrapper.LogEntry{
					Msg:       "panic caught in HTTP request",
					Error:     fmt.Errorf("%v", rec),
					Component: "HTTPServer",
					Method:    "RecoverMiddleware",
				})
				w.WriteHeader(http.StatusInternalServerError)
			}
		}(r.Context())
		next.ServeHTTP(w, r)
	})
}

// LoggingChiMiddleware логирует запрос, добавляет X-Correlation-ID.
func LoggingChiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corrID := xid.New().String()
		ctx := context.WithValue(r.Context(), correlationChiIDCtxKey, corrID)

		w.Header().Set("X-Correlation-ID", corrID)

		lrw := newLoggingChiResponseWriter(w)

		start := time.Now()
		next.ServeHTTP(lrw, r.WithContext(ctx))

		logger.WriteInfoLog(ctx, &logger_wrapper.LogEntry{
			Msg:       fmt.Sprintf("%s %s completed", r.Method, r.URL.Path),
			Component: "HTTPServer",
			Method:    "LoggingMiddleware",
			Args: fmt.Sprintf("status=%d duration=%s ua=%s",
				lrw.statusCode, time.Since(start), r.UserAgent()),
		})
	})
}

type loggingChiResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newLoggingChiResponseWriter(w http.ResponseWriter) *loggingChiResponseWriter {
	return &loggingChiResponseWriter{w, http.StatusOK}
}

func (lrw *loggingChiResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingChiResponseWriter) Write(b []byte) (int, error) {
	return lrw.ResponseWriter.Write(b)
}

func (lrw *loggingChiResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := lrw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("ResponseWriter does not implement http.Hijacker")
	}
	return hj.Hijack()
}
