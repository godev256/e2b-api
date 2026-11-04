package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/buildoshi/e2b-api/internal/models"
)

//go:embed callback.html.tmpl
var callbackPage string

var callbackTemplate = template.Must(template.New("callback").Parse(callbackPage))

// handleLogin starts the authorization code flow with PKCE. The state and the
// verifier travel in short lived HttpOnly cookies, so the server keeps no
// session for a login that may never finish.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(oauthStateBytes)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	verifier, authURL := s.OAuth.NewFlow(state)

	s.setFlowCookie(w, cookieOAuthState, state, cookieTTL)
	s.setFlowCookie(w, cookieOAuthVerifier, verifier, cookieTTL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

type tokenResponse struct {
	AccessToken string         `json:"access_token"`
	TokenType   string         `json:"token_type"`
	ExpiresIn   int            `json:"expires_in"`
	User        callbackUser   `json:"user"`
	Usage       map[string]any `json:"usage,omitempty"`
}

type callbackUser struct {
	ID    string  `json:"id"`
	Email *string `json:"email"`
}

// handleCallback finishes the flow: it checks state, trades the code for
// tokens, signs the user up on first login and shows the access token.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if code := q.Get("error"); code != "" {
		// Only the error code is echoed. The description is provider controlled text.
		s.writeError(w, r, &models.InvalidInputError{Field: "authorization", Message: "the provider returned error " + sanitizeCode(code)})
		return
	}

	stateCookie, stateErr := r.Cookie(cookieOAuthState)
	verifierCookie, verifierErr := r.Cookie(cookieOAuthVerifier)
	if stateErr != nil || verifierErr != nil || !equalConstantTime(stateCookie.Value, q.Get("state")) {
		s.writeError(w, r, &models.InvalidInputError{Field: "state", Message: "login session expired or was tampered with, start again at /auth/login"})
		return
	}
	code := q.Get("code")
	if code == "" {
		s.writeError(w, r, &models.InvalidInputError{Field: "code", Message: "is required"})
		return
	}
	s.clearFlowCookie(w, cookieOAuthState)
	s.clearFlowCookie(w, cookieOAuthVerifier)

	token, err := s.OAuth.Exchange(r.Context(), code, verifierCookie.Value)
	if err != nil {
		s.Log.WarnContext(r.Context(), "code exchange failed", "error", err)
		s.writeError(w, r, &models.InvalidInputError{Field: "code", Message: "could not be exchanged, start again at /auth/login"})
		return
	}
	user, err := s.Auth.Authenticate(r.Context(), token.AccessToken)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	resp := tokenResponse{
		AccessToken: token.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(token.Expiry).Seconds()),
		User:        callbackUser{ID: user.ID.String(), Email: user.Email},
	}
	if wantsHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = callbackTemplate.Execute(w, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) setFlowCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is configurable: off for http://localhost, on behind TLS
		Name:     name,
		Value:    value,
		Path:     cookiePath,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, // Lax: the provider redirects back with a top level GET
	})
}

func (s *Server) clearFlowCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: cookiePath, MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode}) //nolint:gosec // see setFlowCookie
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func equalConstantTime(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// sanitizeCode keeps an OAuth error code printable and short.
func sanitizeCode(code string) string {
	const maxLen = 40
	var b strings.Builder
	for _, c := range code {
		if b.Len() >= maxLen {
			break
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
