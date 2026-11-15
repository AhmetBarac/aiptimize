package config

import "testing"

// TestWriteGateModes covers the inverter write gate: unset means automatic and
// follows the inverter link, while on/off pin it for operators.
func TestWriteGateModes(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		mode    EVControlMode
		healthy map[bool]bool
	}{
		{name: "default is automatic", value: "", mode: EVControlAuto},
		{name: "explicit auto", value: "auto", mode: EVControlAuto},
		{name: "pinned on", value: "on", mode: EVControlOn},
		{name: "boolean true pins on", value: "true", mode: EVControlOn},
		{name: "pinned off", value: "off", mode: EVControlOff},
		{name: "boolean false pins off", value: "false", mode: EVControlOff},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			if tc.value != "" {
				env["AIPTIMIZE_ENABLE_WRITES"] = tc.value
			}
			cfg, err := LoadFromEnv(testEnv(env))
			if err != nil {
				t.Fatalf("LoadFromEnv: %v", err)
			}
			if cfg.WriteMode.Effective() != tc.mode {
				t.Fatalf("mode = %q, want %q", cfg.WriteMode.Effective(), tc.mode)
			}

			switch tc.mode {
			case EVControlOn:
				if !cfg.WritesEnv(false) || !cfg.WritesEnv(true) {
					t.Error("pinned on must allow writes regardless of the link")
				}
			case EVControlOff:
				if cfg.WritesEnv(false) || cfg.WritesEnv(true) {
					t.Error("pinned off must never allow writes")
				}
			default:
				if cfg.WritesEnv(false) {
					t.Error("automatic gate must stay closed while the inverter is silent")
				}
				if !cfg.WritesEnv(true) {
					t.Error("automatic gate must open once the inverter answers")
				}
			}
		})
	}
}
