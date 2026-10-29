// Package forecast contains the PV and house-load forecast model shared by the
// API, dashboard and later the battery planner.
package forecast

import "time"

// StringInfo describes one configured MPPT string in a snapshot.
type StringInfo struct {
	Name    string  `json:"name"`
	KWP     float64 `json:"kwp"`
	Tilt    float64 `json:"tilt"`
	Azimuth float64 `json:"azimuth"`
}

// Point is one 30-minute forecast block.
type Point struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	PVW       float64   `json:"pv_w"`
	PVMinW    float64   `json:"pv_min_w"`
	PVMaxW    float64   `json:"pv_max_w"`
	PVStrings []float64 `json:"pv_strings"`
	LoadW     float64   `json:"load_w"`
}

// Snapshot is the latest combined forecast.
type Snapshot struct {
	GeneratedAt        time.Time    `json:"generated_at"`
	HorizonStart       time.Time    `json:"horizon_start"`
	HorizonEnd         time.Time    `json:"horizon_end"`
	Timezone           string       `json:"timezone"`
	Confidence         float64      `json:"confidence"`
	CalibrationFactor  float64      `json:"calibration_factor"`
	CalibrationSamples int          `json:"calibration_samples"`
	Stale              bool         `json:"stale"`
	PVTotalKWh         float64      `json:"pv_total_kwh"`
	LoadTotalKWh       float64      `json:"load_total_kwh"`
	Strings            []StringInfo `json:"strings"`
	Points             []Point      `json:"points"`
	Warnings           []string     `json:"warnings"`
	Source             string       `json:"source"`
}

// Copy returns a deep copy of the snapshot.
func (s Snapshot) Copy() Snapshot {
	out := s
	out.Strings = append([]StringInfo{}, s.Strings...)
	out.Warnings = append([]string{}, s.Warnings...)
	out.Points = make([]Point, len(s.Points))
	for i, p := range s.Points {
		out.Points[i] = p
		out.Points[i].PVStrings = append([]float64{}, p.PVStrings...)
	}
	return out
}
