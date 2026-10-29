package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Broker fans SSE events out to connected clients. Slow clients drop events
// rather than blocking the producers.
type Broker struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewBroker creates an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[chan []byte]struct{})}
}

// Subscribe registers a listener and returns a cancel function.
func (b *Broker) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish sends a named SSE event with a JSON payload.
func (b *Broker) Publish(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- []byte(frame):
		default:
		}
	}
}

// handleStream serves the SSE stream. It sends an initial snapshot, a status
// event every 15 seconds and a heartbeat comment every 20 seconds.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "Streaming is not supported.", nil)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	fmt.Fprint(w, "retry: 5000\n\n")
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", mustJSON(s.statusSnapshot()))
	flusher.Flush()

	events, cancel := s.broker.Subscribe()
	defer cancel()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	statusTick := time.NewTicker(15 * time.Second)
	defer statusTick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case frame, open := <-events:
			if !open {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-statusTick.C:
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", mustJSON(s.statusSnapshot()))
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return data
}
