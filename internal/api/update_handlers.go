package api

import (
	"context"
	"net/http"
	"time"
)

// handleUpdateCheck asks the registry for the newest published build and
// compares it with the running version.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update_unavailable",
			"Update checks are not configured.", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	status, err := s.updates.Check(ctx, s.cfg.Version)
	if err != nil {
		s.log.Warn("update check failed", "err", err)
		writeError(w, http.StatusBadGateway, "registry_unavailable",
			"Could not reach the container registry.", map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleUpdateApply asks the host to pull the published image and restart. The
// process cannot replace its own container, so a supervisor watches for the
// request file.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if s.requestUpdate == nil {
		writeError(w, http.StatusServiceUnavailable, "update_unavailable",
			"This deployment cannot install updates from the app.", nil)
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required",
			"Installing an update requires confirm: true.", nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"updating": true})
	go func() {
		// Give the response time to reach the client; the restart follows.
		time.Sleep(1500 * time.Millisecond)
		if err := s.requestUpdate(); err != nil {
			s.log.Error("could not request an update", "err", err)
			return
		}
		s.log.Info("update requested from the UI")
	}()
}
