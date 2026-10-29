package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	status := s.statusSnapshot(r.Context())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP aiptimize_up Whether the application is running.\n")
	fmt.Fprintf(w, "# TYPE aiptimize_up gauge\naiptimize_up 1\n")
	fmt.Fprintf(w, "# TYPE aiptimize_uptime_seconds gauge\naiptimize_uptime_seconds %d\n", status["uptime_s"])
	if telemetry, ok := status["telemetry"].(map[string]any); ok {
		if available, _ := telemetry["available"].(bool); available {
			fmt.Fprintf(w, "aiptimize_telemetry_available 1\n")
		} else {
			fmt.Fprintf(w, "aiptimize_telemetry_available 0\n")
		}
	}
	if prices, ok := status["prices"].(map[string]any); ok {
		if available, _ := prices["available"].(bool); available {
			fmt.Fprintf(w, "aiptimize_prices_available 1\n")
		} else {
			fmt.Fprintf(w, "aiptimize_prices_available 0\n")
		}
	}
	if plan, ok := status["plan"].(map[string]any); ok {
		if available, _ := plan["available"].(bool); available {
			fmt.Fprintf(w, "aiptimize_plan_available 1\n")
			if summary, ok := plan["summary"].(map[string]any); ok {
				if savings, ok := summary["savings"].(float64); ok {
					fmt.Fprintf(w, "aiptimize_plan_savings %g\n", savings)
				}
				if cost, ok := summary["cost"].(float64); ok {
					fmt.Fprintf(w, "aiptimize_plan_cost %g\n", cost)
				}
			}
		} else {
			fmt.Fprintf(w, "aiptimize_plan_available 0\n")
		}
	}
	if ev, ok := status["ev"].(map[string]any); ok {
		if available, _ := ev["available"].(bool); available {
			fmt.Fprintf(w, "aiptimize_ev_available 1\n")
			if charger, ok := ev["status"].(map[string]any); ok {
				if power, ok := charger["power_w"].(float64); ok {
					fmt.Fprintf(w, "aiptimize_ev_power_watts %g\n", power)
				}
			}
		} else {
			fmt.Fprintf(w, "aiptimize_ev_available 0\n")
		}
	}
	fmt.Fprintf(w, "# TYPE aiptimize_generated_timestamp_seconds gauge\naiptimize_generated_timestamp_seconds %d\n", time.Now().Unix())
}
