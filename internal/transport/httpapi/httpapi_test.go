package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/buildoshi/e2b-api/internal/models"
	transport "github.com/buildoshi/e2b-api/internal/transport/httpapi"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/idgen"
)

const validToken = "valid-token"

var testUser = models.User{ID: idgen.New(), Subject: "foo@bar.com"}

type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, token string) (models.User, error) {
	if token != validToken {
		return models.User{}, models.ErrUnauthenticated
	}
	return testUser, nil
}

// fakeSandboxes returns whatever error the test sets.
type fakeSandboxes struct{ err error }

func (f fakeSandboxes) Create(context.Context, uuid.UUID, uuid.UUID, string) (models.Sandbox, error) {
	return models.Sandbox{ID: idgen.New(), Status: models.SandboxRunning, Template: "base"}, f.err
}

func (f fakeSandboxes) List(context.Context, uuid.UUID, uuid.UUID, models.Page) (usecase.Paged[models.Sandbox], error) {
	return usecase.Paged[models.Sandbox]{}, f.err
}

func (f fakeSandboxes) Stop(context.Context, uuid.UUID, uuid.UUID) (models.Sandbox, error) {
	return models.Sandbox{}, f.err
}

type fakeProjects struct{ transport.ProjectService }

func (fakeProjects) Create(context.Context, uuid.UUID, string) (models.Project, error) {
	return models.Project{ID: idgen.New(), Name: "p", PlanCode: models.PlanFree}, nil
}

func newServer(t *testing.T, sandboxErr error) http.Handler {
	t.Helper()
	return transport.NewHandler(
		transport.Config{RequestTimeout: time.Second, MaxBodyBytes: 1 << 10},
		transport.Deps{
			Auth:      fakeAuth{},
			Projects:  fakeProjects{},
			Sandboxes: fakeSandboxes{err: sandboxErr},
			OAuth:     fakeOAuth{},
			Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	)
}

type fakeOAuth struct{}

func (fakeOAuth) NewFlow(string) (string, string) { return "v", "http://hydra/auth" }
func (fakeOAuth) Exchange(context.Context, string, string) (*oauth2.Token, error) {
	return nil, nil
}

func do(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func authed(extra map[string]string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + validToken}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code
}

func TestProtectedRoutesRequireAValidBearerToken(t *testing.T) {
	h := newServer(t, nil)
	for name, headers := range map[string]map[string]string{
		"missing":      nil,
		"wrong scheme": {"Authorization": "Basic abc"},
		"bad token":    {"Authorization": "Bearer nope"},
		"empty bearer": {"Authorization": "Bearer "},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, "/v1/projects", "", headers)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Equal(t, "unauthenticated", errorCode(t, rec))
			require.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
		})
	}
}

func TestErrorsMapToStableStatusesAndCodes(t *testing.T) {
	path := "/v1/projects/" + uuid.NewString() + "/sandboxes"
	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"not found":      {models.ErrNotFound, http.StatusNotFound, "not_found"},
		"sandbox quota":  {&models.SandboxQuotaError{Limit: 3, Running: 3}, http.StatusTooManyRequests, "sandbox_quota_exceeded"},
		"rate limited":   {&models.RateLimitError{Decision: models.RateDecision{Limit: 10, RetryAfter: 1200 * time.Millisecond}}, http.StatusTooManyRequests, "rate_limit_exceeded"},
		"invalid input":  {&models.InvalidInputError{Field: "template", Message: "bad"}, http.StatusBadRequest, "invalid_request"},
		"forbidden":      {models.ErrForbidden, http.StatusForbidden, "forbidden"},
		"project quota":  {&models.ProjectQuotaError{Limit: 10, Owned: 10}, http.StatusTooManyRequests, "project_quota_exceeded"},
		"auth down":      {models.ErrAuthUnavailable, http.StatusServiceUnavailable, "service_unavailable"},
		"lock timeout":   {pgState("55P03"), http.StatusServiceUnavailable, "busy"},
		"stmt timeout":   {pgState("57014"), http.StatusServiceUnavailable, "busy"},
		"unique clash":   {pgState("23505"), http.StatusConflict, "conflict"},
		"bad data":       {pgState("22021"), http.StatusBadRequest, "invalid_request"},
		"other pg error": {pgState("42P01"), http.StatusInternalServerError, "internal_error"},
		"unexpected":     {context.DeadlineExceeded, http.StatusInternalServerError, "internal_error"},
		"secret details": {io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := do(t, newServer(t, tc.err), http.MethodPost, path, `{}`, authed(map[string]string{"Content-Type": "application/json"}))
			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantCode, errorCode(t, rec))
			require.NotContains(t, rec.Body.String(), "unexpected EOF", "internal error text must not leak")
		})
	}
}

// pgState mimics a database error that exposes its SQLSTATE.
type pgState string

func (e pgState) Error() string    { return "pg error " + string(e) }
func (e pgState) SQLState() string { return string(e) }

func TestBusyResponseTellsClientsWhenToRetry(t *testing.T) {
	rec := do(t, newServer(t, pgState("55P03")), http.MethodPost, "/v1/projects/"+uuid.NewString()+"/sandboxes", `{}`,
		authed(map[string]string{"Content-Type": "application/json"}))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestAccessLogNamesTheMatchedRoute(t *testing.T) {
	var logs strings.Builder
	h := transport.NewHandler(
		transport.Config{RequestTimeout: time.Second, MaxBodyBytes: 1 << 10},
		transport.Deps{
			Auth: fakeAuth{}, Projects: fakeProjects{}, Sandboxes: fakeSandboxes{}, OAuth: fakeOAuth{},
			Log: slog.New(slog.NewJSONHandler(&logs, nil)),
		},
	)

	do(t, h, http.MethodDelete, "/v1/sandboxes/"+uuid.NewString(), "", authed(nil))

	require.Contains(t, logs.String(), `"route":"DELETE /v1/sandboxes/{id}"`, "the pattern, so ids stay out of the log")
	require.Contains(t, logs.String(), `"duration_ms"`)
}

func TestRateLimitResponseCarriesBackoffHeaders(t *testing.T) {
	err := &models.RateLimitError{Decision: models.RateDecision{Limit: 10, Remaining: 0, RetryAfter: 1200 * time.Millisecond, ResetAfter: 2 * time.Second}}

	rec := do(t, newServer(t, err), http.MethodGet, "/v1/projects/"+uuid.NewString()+"/sandboxes", "", authed(nil))

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "2", rec.Header().Get("Retry-After"), "rounded up to whole seconds")
	require.Equal(t, "10", rec.Header().Get("X-RateLimit-Limit"))
	require.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
}

func TestMalformedIDsLookLikeMissingResources(t *testing.T) {
	rec := do(t, newServer(t, nil), http.MethodDelete, "/v1/sandboxes/not-a-uuid", "", authed(nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "not_found", errorCode(t, rec))
}

func TestJSONBodiesAreStrict(t *testing.T) {
	h := newServer(t, nil)
	json := map[string]string{"Content-Type": "application/json"}

	tests := map[string]struct {
		body    string
		headers map[string]string
		want    int
	}{
		"valid":              {`{"name":"demo"}`, authed(json), http.StatusCreated},
		"unknown field":      {`{"name":"demo","admin":true}`, authed(json), http.StatusBadRequest},
		"trailing data":      {`{"name":"demo"} {"x":1}`, authed(json), http.StatusBadRequest},
		"not json":           {`name=demo`, authed(json), http.StatusBadRequest},
		"wrong content type": {`{"name":"demo"}`, authed(map[string]string{"Content-Type": "text/plain"}), http.StatusUnsupportedMediaType},
		"too large":          {`{"name":"` + strings.Repeat("a", 4<<10) + `"}`, authed(json), http.StatusRequestEntityTooLarge},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, "/v1/projects", tc.body, tc.headers)
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestRequestIDIsEchoedOrGeneratedAndSanitized(t *testing.T) {
	h := newServer(t, nil)

	echoed := do(t, h, http.MethodGet, "/healthz", "", map[string]string{"X-Request-Id": "trace-123"})
	require.Equal(t, "trace-123", echoed.Header().Get("X-Request-Id"))

	replaced := do(t, h, http.MethodGet, "/healthz", "", map[string]string{"X-Request-Id": "bad id\nwith newline"})
	_, err := uuid.Parse(replaced.Header().Get("X-Request-Id"))
	require.NoError(t, err, "an unsafe id is replaced by a generated one")
}

func TestUnknownRoutesReturnJSON(t *testing.T) {
	rec := do(t, newServer(t, nil), http.MethodGet, "/nope", "", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "not_found", errorCode(t, rec))
}

func TestReadyzReportsDrainingSoLoadBalancersStopSendingTraffic(t *testing.T) {
	var draining atomic.Bool
	h := transport.NewHandler(
		transport.Config{RequestTimeout: time.Second, MaxBodyBytes: 1 << 10},
		transport.Deps{
			Auth: fakeAuth{}, Projects: fakeProjects{}, Sandboxes: fakeSandboxes{}, OAuth: fakeOAuth{},
			Draining: &draining, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	)

	require.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/readyz", "", nil).Code)

	draining.Store(true)
	rec := do(t, h, http.MethodGet, "/readyz", "", nil)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "draining")
	require.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/healthz", "", nil).Code, "liveness is unaffected")
}
