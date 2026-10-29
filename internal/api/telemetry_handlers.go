package api

import (
	"context"
	"net/http"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
)

// telemetryStatus reports the latest inverter sample and its freshness.
func (s *Server) telemetryStatus() map[string]any {
	out := map[string]any{
		"available": false,
		"stale":     true,
		"sample":    nil,
	}
	if s.telemetry == nil {
		return out
	}
	sample, receivedAt, ok := s.telemetry.Latest()
	if !ok {
		return out
	}
	interval := s.settings.Get().Inverter.TelemetryIntervalS
	if interval < 5 {
		interval = 30
	}
	staleAfter := 3 * time.Duration(interval) * time.Second
	if staleAfter < 90*time.Second {
		staleAfter = 90 * time.Second
	}
	age := s.now().Sub(receivedAt)
	out["available"] = true
	out["stale"] = age > staleAfter
	out["age_s"] = int(age.Seconds())
	out["updated_at"] = receivedAt.UTC()
	out["sample"] = sample
	return out
}

type testInverterResponse struct {
	OK        bool              `json:"ok"`
	Identity  inverter.Identity `json:"identity"`
	Telemetry domain.Telemetry  `json:"telemetry"`
}

// handleTestInverter performs a read-only connectivity check. It never writes
// to the dongle and requires no hardware arm gate.
func (s *Server) handleTestInverter(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "inverter_unavailable", "Telemetry is not configured.", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	identity, reading, err := s.telemetry.Test(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "inverter_unreachable", "Could not read the inverter.", map[string]any{
			"error": err.Error(),
		})
		return
	}
	sample := domain.Telemetry{
		TS:           reading.At.UTC(),
		SOC:          reading.SOC,
		PVW:          reading.PVW,
		PVStrings:    reading.PVStrings,
		GridW:        reading.GridW,
		BatteryW:     reading.BatteryW,
		LoadW:        reading.LoadW,
		InverterMode: reading.Mode,
		Extra:        reading.Extra,
	}
	writeJSON(w, http.StatusOK, testInverterResponse{OK: true, Identity: identity, Telemetry: sample})
}
