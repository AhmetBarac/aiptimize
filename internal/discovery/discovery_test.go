package discovery

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func newTestResponder(t *testing.T, now func() time.Time) (*Responder, *net.UDPAddr) {
	t.Helper()
	r, err := New(Options{
		Port:     0,
		HTTPPort: 8080,
		Info: func() Info {
			return Info{ID: "11111111-2222-4333-8444-555555555555", Name: "BaracHome", Version: "dev-0.13.0"}
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); r.Close() })
	go func() { _ = r.Run(ctx) }()

	local := r.Addr().(*net.UDPAddr)
	return r, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: local.Port}
}

func probeOnce(t *testing.T, dst *net.UDPAddr, payload []byte, timeout time.Duration) ([]byte, error) {
	t.Helper()
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.WriteToUDP(payload, dst); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1024)
	n, _, err := client.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func TestResponderAnswersProbe(t *testing.T) {
	_, dst := newTestResponder(t, time.Now)

	body, err := probeOnce(t, dst, []byte(`{"aiptimize":"discover","v":1}`), 2*time.Second)
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	var got reply
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Aiptimize != "server" || got.V != 1 {
		t.Fatalf("unexpected reply: %+v", got)
	}
	if got.InstanceID != "11111111-2222-4333-8444-555555555555" || got.Name != "BaracHome" || got.Version != "dev-0.13.0" {
		t.Fatalf("unexpected info: %+v", got)
	}
	if got.API != "http://127.0.0.1:8080" {
		t.Fatalf("api = %q", got.API)
	}
}

func TestResponderIgnoresForeignAndMalformedPackets(t *testing.T) {
	_, dst := newTestResponder(t, time.Now)
	for _, payload := range [][]byte{
		[]byte(`not json`),
		[]byte(`{"aiptimize":"something-else","v":1}`),
		[]byte(`{"aiptimize":"discover","v":2}`),
		[]byte(`{}`),
	} {
		if _, err := probeOnce(t, dst, payload, 300*time.Millisecond); err == nil {
			t.Fatalf("unexpected reply for %s", payload)
		}
	}
}

func TestResponderRateLimitsPerSource(t *testing.T) {
	now := time.Now()
	current := now
	_, dst := newTestResponder(t, func() time.Time { return current })

	if _, err := probeOnce(t, dst, []byte(`{"aiptimize":"discover","v":1}`), 2*time.Second); err != nil {
		t.Fatalf("first probe: %v", err)
	}
	// Same source, same second: dropped.
	if _, err := probeOnce(t, dst, []byte(`{"aiptimize":"discover","v":1}`), 300*time.Millisecond); err == nil {
		t.Fatal("second probe within the rate window was answered")
	}
	// After the window it is answered again.
	current = now.Add(2 * time.Second)
	if _, err := probeOnce(t, dst, []byte(`{"aiptimize":"discover","v":1}`), 2*time.Second); err != nil {
		t.Fatalf("probe after the window: %v", err)
	}
}
