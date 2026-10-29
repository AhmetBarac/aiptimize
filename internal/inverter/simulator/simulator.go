// Package simulator provides a deterministic in-process inverter and an HTTP
// fixture server speaking the Solplanet AI-Dongle read endpoints.
package simulator

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/inverter/solplanet"
)

// Config configures the simulated installation.
type Config struct {
	Serial        string
	Strings       int
	PeakW         float64
	CapacityKWh   float64
	MaxChargeW    float64
	MaxDischargeW float64
	StartSOC      float64
	Location      *time.Location
	Now           func() time.Time
	Signs         inverter.Signs
}

// Inverter is a deterministic simulated battery/PV installation.
type Inverter struct {
	cfg Config

	mu        sync.Mutex
	soc       float64
	last      time.Time
	importKWh float64
	exportKWh float64
	schedule  *domain.Schedule
}

// New builds a simulator with sensible defaults.
func New(cfg Config) *Inverter {
	if cfg.Serial == "" {
		cfg.Serial = "SIM-ASW08KH-0001"
	}
	if cfg.Strings < 1 || cfg.Strings > 3 {
		cfg.Strings = 2
	}
	if cfg.PeakW <= 0 {
		cfg.PeakW = 6000
	}
	if cfg.CapacityKWh <= 0 {
		cfg.CapacityKWh = 15.36
	}
	if cfg.MaxChargeW <= 0 {
		cfg.MaxChargeW = 6000
	}
	if cfg.MaxDischargeW <= 0 {
		cfg.MaxDischargeW = 3000
	}
	if cfg.StartSOC <= 0 || cfg.StartSOC > 100 {
		cfg.StartSOC = 55
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Inverter{cfg: cfg, soc: cfg.StartSOC, last: cfg.Now()}
}

// Identity implements inverter.Reader.
func (s *Inverter) Identity(context.Context) (inverter.Identity, error) {
	return inverter.Identity{
		Serial:      s.cfg.Serial,
		Model:       "ASW08kH-T2 (simulator)",
		Firmware:    "SIM-1.0",
		RatedPowerW: 8000,
		Phases:      3,
		SafetyCode:  87,
		CapacityKWh: s.cfg.CapacityKWh,
		WorkMode:    4,
		MinSOC:      15,
		MaxSOC:      100,
	}, nil
}

// Read advances and returns one simulated sample.
func (s *Inverter) Read(context.Context) (inverter.Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.cfg.Now()
	local := now.In(s.cfg.Location)
	hour := float64(local.Hour()) + float64(local.Minute())/60 + float64(local.Second())/3600
	dayFactor := 0.85 + 0.1*math.Sin(float64(local.YearDay())/365*2*math.Pi)
	solar := 0.0
	if hour > 6 && hour < 18 {
		solar = math.Sin(math.Pi * (hour - 6) / 12)
	}
	pvW := math.Max(0, s.cfg.PeakW*solar*dayFactor)
	loadW := 450 + 180*math.Sin(2*math.Pi*(hour-7)/24) + 90*math.Sin(4*math.Pi*(hour-18)/24)
	if loadW < 150 {
		loadW = 150
	}

	// Update SOC from the previous interval using the current net power.
	elapsed := now.Sub(s.last).Hours()
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > 1 {
		elapsed = 1
	}
	net := pvW - loadW
	batteryW := 0.0
	if net > 0 {
		batteryW = math.Min(net, s.cfg.MaxChargeW)
		headroom := (100 - s.soc) / 100 * s.cfg.CapacityKWh * 1000
		batteryW = math.Min(batteryW, headroom/math.Max(elapsed, 1.0/60))
	} else {
		available := math.Max(0, (s.soc-10)/100*s.cfg.CapacityKWh*1000)
		batteryW = -math.Min(-net, s.cfg.MaxDischargeW)
		batteryW = math.Max(batteryW, -available/math.Max(elapsed, 1.0/60))
	}
	if elapsed > 0 {
		deltaKWh := batteryW * elapsed / 1000 * 0.95
		s.soc += deltaKWh / s.cfg.CapacityKWh * 100
		if s.soc > 100 {
			s.soc = 100
		}
		if s.soc < 0 {
			s.soc = 0
		}
	}
	s.last = now

	gridW := loadW + batteryW - pvW
	if gridW > 0 {
		s.importKWh += gridW * elapsed / 1000
	} else {
		s.exportKWh += -gridW * elapsed / 1000
	}

	signPV := signOr(s.cfg.Signs.PV)
	signGrid := signOr(s.cfg.Signs.Grid)
	signBattery := signOr(s.cfg.Signs.Battery)
	stringsW := make([]float64, s.cfg.Strings)
	weights := []float64{0.6, 0.4, 0.25}
	var weightSum float64
	for i := 0; i < s.cfg.Strings; i++ {
		weightSum += weights[i]
	}
	for i := 0; i < s.cfg.Strings; i++ {
		stringsW[i] = round1(pvW * weights[i] / weightSum * signPV)
	}

	bst := 1
	if batteryW > 50 {
		bst = 2
	} else if batteryW < -50 {
		bst = 3
	}
	reading := inverter.Reading{
		At:        now.UTC(),
		PVW:       round1(pvW * signPV),
		PVStrings: stringsW,
		GridW:     round1(gridW * signGrid),
		BatteryW:  round1(batteryW * signBattery),
		SOC:       round1(s.soc),
		SOH:       98,
		Mode:      4,
		Extra: map[string]any{
			"bst":             bst,
			"soh":             98,
			"battery_temp_c":  round1(22 + 3*math.Sin(hour/4)),
			"inverter_temp_c": round1(30 + 8*math.Max(0, solar)),
			"pv_ac_w":         round1(pvW),
			"simulator":       true,
		},
	}
	reading.LoadW = round1(reading.PVW + reading.GridW - reading.BatteryW)
	if reading.LoadW < 0 {
		reading.LoadW = 0
	}
	return reading, nil
}

// Schedule returns the current weekly schedule. It starts empty.
func (s *Inverter) Schedule(context.Context) (domain.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schedule != nil {
		return cloneSchedule(*s.schedule), nil
	}
	days := make(map[string][]domain.ScheduleSlot, len(dayKeys))
	for _, day := range dayKeys {
		days[day] = []domain.ScheduleSlot{}
	}
	return domain.Schedule{Days: days, PinW: 6000, PoutW: 3000}, nil
}

// WriteSchedule validates and stores a schedule, returning the readback.
func (s *Inverter) WriteSchedule(_ context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	if _, err := solplanet.EncodeSchedule(schedule); err != nil {
		return domain.Schedule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := cloneSchedule(schedule)
	s.schedule = &copy
	return cloneSchedule(copy), nil
}

// Close implements inverter.Reader.
func (s *Inverter) Close() error { return nil }

var dayKeys = []string{"Mon", "Tus", "Wen", "Thu", "Fri", "Sat", "Sun"}

// Handler serves the dongle HTTP API for tests and manual demos. setdefine and
// setbattery are accepted; the simulator is the only writable target in tests.
func (s *Inverter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/setting.cgi" {
			s.handleSetting(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"dat":"failed","error":"simulator accepts only setdefine/setbattery"}`))
			return
		}
		switch r.URL.Path {
		case "/getdev.cgi":
			s.handleGetDev(w, r)
		case "/getdevdata.cgi":
			s.handleGetDevData(w, r)
		case "/getdefine.cgi":
			s.handleGetDefine(w)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *Inverter) handleGetDev(w http.ResponseWriter, r *http.Request) {
	device := r.URL.Query().Get("device")
	switch device {
	case "2":
		writeJSON(w, map[string]any{
			"inv": []map[string]any{{
				"isn": s.cfg.Serial, "model": "ASW08kH-T2", "msw": "SIM-1.0",
				"rate": 8000, "add": 3, "safety": 87,
			}},
			"num": 1,
		})
	case "4":
		writeJSON(w, map[string]any{
			"isn": s.cfg.Serial, "type": 1, "mod_r": 4, "muf": 5, "mod": 9, "num": 1,
			"discharge_max": 15, "charge_max": 100,
			"battery": map[string]any{"capacity": s.cfg.CapacityKWh * 1000, "softwarever": "SIM-BMS-1.0"},
		})
	default:
		http.NotFound(w, r)
	}
}

func (s *Inverter) handleGetDevData(w http.ResponseWriter, r *http.Request) {
	reading, err := s.Read(r.Context())
	if err != nil {
		writeJSON(w, map[string]any{"dat": "failed"})
		return
	}
	local := s.cfg.Now().In(s.cfg.Location)
	tim := local.Format("20060102150405")
	switch r.URL.Query().Get("device") {
	case "2":
		vpv := make([]int, len(reading.PVStrings))
		ipv := make([]int, len(reading.PVStrings))
		for i, w := range reading.PVStrings {
			vpv[i] = 3500
			ipv[i] = int(math.Round(math.Abs(w) / 350 * 100))
		}
		writeJSON(w, map[string]any{
			"flg": 1, "tim": tim, "tmp": 400, "pac": int(math.Round(reading.PVW)),
			"eto": 80428, "etd": 374, "err": 0, "vac": []int{2367, 2371, 2351},
			"vpv": vpv, "ipv": ipv, "pac1": 0, "pac2": 0, "pac3": 0,
		})
	case "3":
		writeJSON(w, map[string]any{
			"flg": 1, "tim": tim, "pac": int(math.Round(reading.GridW)),
			"itd": 966, "otd": 2217, "iet": 180581, "oet": 38773, "mod": 1,
		})
	case "4":
		writeJSON(w, map[string]any{
			"flg": 1, "tim": tim, "ppv": int(math.Round(reading.PVW)),
			"etdpv": 489, "etopv": 88453, "cst": 10, "bst": reading.Extra["bst"],
			"vb": 31700, "cb": 69, "pb": int(math.Round(-reading.BatteryW)),
			"tb": 265, "soc": int(math.Round(reading.SOC)), "soh": 98,
			"cli": 150, "clo": 300, "ebi": 162, "ebo": 72,
		})
	default:
		http.NotFound(w, r)
	}
}

func (s *Inverter) handleGetDefine(w http.ResponseWriter) {
	schedule, err := s.Schedule(context.Background())
	if err != nil {
		writeJSON(w, map[string]any{"dat": "failed", "error": err.Error()})
		return
	}
	body, err := solplanet.EncodeSchedule(schedule)
	if err != nil {
		writeJSON(w, map[string]any{"dat": "failed", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func (s *Inverter) handleSetting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Device int             `json:"device"`
		Action string          `json:"action"`
		Value  json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"dat":"failed","error":"invalid request"}`))
		return
	}
	switch req.Action {
	case "setdefine":
		schedule, warnings, err := solplanet.DecodeSchedule(req.Value)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"dat":"failed","error":"invalid schedule"}`))
			return
		}
		if _, err := s.WriteSchedule(r.Context(), schedule); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"dat":"failed","error":"invalid schedule"}`))
			return
		}
		_ = warnings
		writeJSON(w, map[string]any{"dat": "success"})
	case "setbattery":
		writeJSON(w, map[string]any{"dat": "success"})
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"dat":"failed","error":"unsupported action"}`))
	}
}

func cloneSchedule(s domain.Schedule) domain.Schedule {
	out := domain.Schedule{Days: make(map[string][]domain.ScheduleSlot, len(s.Days)), PinW: s.PinW, PoutW: s.PoutW}
	for day, slots := range s.Days {
		out.Days[day] = append([]domain.ScheduleSlot(nil), slots...)
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func signOr(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
