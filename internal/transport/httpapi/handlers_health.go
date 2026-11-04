package httpapi

import (
	"context"
	"net/http"
	"sync"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady runs every dependency check in parallel. Orchestrators use it to
// decide whether to route traffic here, so it fails when a dependency is down.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.Draining != nil && s.Draining.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		return
	}
	results := make(map[string]string, len(s.Readiness))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, check := range s.Readiness {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
			defer cancel()

			status := "ok"
			if err := check(ctx); err != nil {
				status = "unavailable"
				s.Log.WarnContext(r.Context(), "readiness check failed", "check", name, "error", err)
			}
			mu.Lock()
			results[name] = status
			mu.Unlock()
		}()
	}
	wg.Wait()

	code, overall := http.StatusOK, "ok"
	for _, status := range results {
		if status != "ok" {
			code, overall = http.StatusServiceUnavailable, "unavailable"
		}
	}

	writeJSON(w, code, map[string]any{"status": overall, "checks": results})
}
