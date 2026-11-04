package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buildoshi/e2b-api/internal/models"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// writeError maps a domain error to an HTTP response. Anything it does not
// recognize becomes a generic 500: internals never reach the client.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, payload := s.classify(r, err)
	payload.RequestID = requestIDFrom(r.Context())

	if status >= http.StatusInternalServerError {
		s.Log.ErrorContext(r.Context(), "request failed",
			slog.String("request_id", payload.RequestID), slog.Any("error", err))
	}
	var rate *models.RateLimitError
	if errors.As(err, &rate) {
		setRateLimitHeaders(w, rate.Decision)
	}
	if payload.Code == codeBusy {
		w.Header().Set(headerRetryAfter, busyRetryAfter)
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="e2b-api"`)
	}
	writeJSON(w, status, errorBody{Error: payload})
}

func (s *Server) classify(r *http.Request, err error) (int, errorPayload) {
	var (
		invalid      *models.InvalidInputError
		unknown      *models.UserNotRegisteredError
		quota        *models.SandboxQuotaError
		projectQuota *models.ProjectQuotaError
		rate         *models.RateLimitError
		tooLarge     *http.MaxBytesError
		sqlState     interface{ SQLState() string }
	)
	switch {
	case errors.As(err, &invalid):
		return http.StatusBadRequest, errorPayload{Code: codeInvalidRequest, Message: invalid.Error()}
	case errors.Is(err, errUnsupportedMedia):
		return http.StatusUnsupportedMediaType, errorPayload{Code: codeUnsupportedMedia, Message: err.Error()}
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, errorPayload{Code: codeRequestTooLarge, Message: "request body is too large"}
	case errors.Is(err, models.ErrUnauthenticated):
		return http.StatusUnauthorized, errorPayload{Code: codeUnauthenticated, Message: "missing or invalid access token"}
	case errors.Is(err, models.ErrNotFound):
		return http.StatusNotFound, errorPayload{Code: codeNotFound, Message: "resource not found"}
	case errors.Is(err, models.ErrForbidden):
		return http.StatusForbidden, errorPayload{Code: codeForbidden, Message: "only project owners can do this"}
	case errors.As(err, &unknown):
		return http.StatusNotFound, errorPayload{Code: codeUserNotFound, Message: unknown.Error()}
	case errors.As(err, &quota):
		return http.StatusTooManyRequests, errorPayload{
			Code:    codeSandboxQuota,
			Message: "the project reached the running sandbox limit of its plan, stop a sandbox or upgrade the plan",
			Details: map[string]any{"limit": quota.Limit, "running": quota.Running},
		}
	case errors.As(err, &projectQuota):
		return http.StatusTooManyRequests, errorPayload{
			Code:    codeProjectQuota,
			Message: "you own the maximum number of projects",
			Details: map[string]any{"limit": projectQuota.Limit, "owned": projectQuota.Owned},
		}
	case errors.As(err, &rate):
		return http.StatusTooManyRequests, errorPayload{
			Code:    codeRateLimited,
			Message: "too many requests for this plan, retry after the delay in the Retry-After header",
			Details: map[string]any{
				"limit":               rate.Decision.Limit,
				"retry_after_seconds": ceilSeconds(rate.Decision.RetryAfter),
			},
		}
	case errors.Is(err, models.ErrAuthUnavailable):
		return http.StatusServiceUnavailable, errorPayload{Code: codeUnavailable, Message: "authentication is temporarily unavailable"}
	case errors.As(err, &sqlState):
		if status, payload, ok := classifySQLState(sqlState.SQLState()); ok {
			return status, payload
		}
		return http.StatusInternalServerError, errorPayload{Code: codeInternal, Message: "internal server error"}
	case r.Context().Err() != nil:
		return http.StatusServiceUnavailable, errorPayload{Code: codeUnavailable, Message: "request was cancelled or timed out"}
	default:
		return http.StatusInternalServerError, errorPayload{Code: codeInternal, Message: "internal server error"}
	}
}

// classifySQLState explains database failures a client can act on. They are
// recognized by SQLSTATE through an interface, so this layer needs no driver.
func classifySQLState(state string) (int, errorPayload, bool) {
	switch {
	case state == sqlStateLockNotAvailable || state == sqlStateQueryCanceled:
		return http.StatusServiceUnavailable, errorPayload{Code: codeBusy, Message: "the resource is busy, retry shortly"}, true
	case state == sqlStateUniqueViolation:
		return http.StatusConflict, errorPayload{Code: codeConflict, Message: "the request conflicts with existing data"}, true
	case strings.HasPrefix(state, sqlStateDataExceptionCls):
		return http.StatusBadRequest, errorPayload{Code: codeInvalidRequest, Message: "a value in the request was not accepted"}, true
	default:
		return 0, errorPayload{}, false
	}
}

// setRateLimitHeaders exposes the bucket state so clients can back off.
func setRateLimitHeaders(w http.ResponseWriter, d models.RateDecision) {
	h := w.Header()
	h.Set(headerRetryAfter, strconv.Itoa(ceilSeconds(d.RetryAfter)))
	h.Set(headerRateLimit, strconv.Itoa(d.Limit))
	h.Set(headerRateRemain, strconv.Itoa(d.Remaining))
	h.Set(headerRateReset, strconv.Itoa(ceilSeconds(d.ResetAfter)))
}

func ceilSeconds(d time.Duration) int {
	return int(math.Ceil(d.Seconds()))
}
