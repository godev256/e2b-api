package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxRoute
)

// routeHolder lets the router tell the access log which route matched. The
// log middleware sits outside the router, and the router only sets the
// pattern on its own copy of the request, so the pattern travels by pointer.
type routeHolder struct{ pattern string }

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

// chain applies the cross cutting middleware. The list runs from the innermost
// to the outermost, so a request meets them in reverse: request id first, then
// the access log, then panic recovery. The log sits outside recovery on
// purpose, so a request that panicked is still logged, as the 500 it became.
func (s *Server) chain(next http.Handler) http.Handler {
	for _, wrap := range []func(http.Handler) http.Handler{
		s.limitBody,
		s.timeout,
		s.securityHeaders,
		s.recoverPanic,
		s.accessLog,
		s.requestID,
	} {
		next = wrap(next)
	}
	return next
}

// recordRoute stores the matched route for the access log, then serves.
func recordRoute(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if holder, ok := r.Context().Value(ctxRoute).(*routeHolder); ok {
			_, holder.pattern = mux.Handler(r)
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(headerRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

// validRequestID accepts only short, plain identifiers so a hostile header
// cannot inject newlines or flood the logs.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity
					panic(rec)
				}
				s.Log.ErrorContext(r.Context(), "panic in handler", slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
				s.writeError(w, r, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		holder := &routeHolder{}
		r = r.WithContext(context.WithValue(r.Context(), ctxRoute, holder))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		s.Log.LogAttrs(r.Context(), levelFor(r, holder.pattern, rec.status), "request",
			slog.String("request_id", requestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("route", holder.pattern), // the pattern, not the path: keeps ids out of log cardinality
			slog.Int("status", rec.status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/microsPerMilli),
		)
	})
}

// levelFor keeps probes quiet: the Docker healthcheck calls every few seconds
// and would otherwise drown real traffic.
func levelFor(_ *http.Request, route string, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case strings.HasSuffix(route, "/healthz") || strings.HasSuffix(route, "/readyz"):
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

const microsPerMilli = 1000.0

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) timeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

type authedHandler func(w http.ResponseWriter, r *http.Request, user models.User)

// authenticated requires a valid bearer token and passes the caller to h.
func (s *Server) authenticated(h authedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
		if !ok || token == "" {
			s.writeError(w, r, models.ErrUnauthenticated)
			return
		}
		user, err := s.Auth.Authenticate(r.Context(), token)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		h(w, r, user)
	})
}
