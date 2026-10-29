package api

import (
	"encoding/json"
	"net/http"

	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func (s *Server) handleExportSettings(w http.ResponseWriter, r *http.Request) {
	sections, err := s.settings.JSON()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="aiptimize-settings.json"`)
	writeJSON(w, http.StatusOK, sections)
}

func (s *Server) handleImportSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]json.RawMessage
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	applied := []string{}
	for _, section := range settings.Sections {
		raw, ok := payload[section]
		if !ok {
			continue
		}
		if _, err := s.settings.Patch(r.Context(), section, raw); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_settings", err.Error(),
				map[string]any{"section": section, "applied": applied})
			return
		}
		applied = append(applied, section)
	}
	if len(applied) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_settings", "No recognised settings sections.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": applied})
}
