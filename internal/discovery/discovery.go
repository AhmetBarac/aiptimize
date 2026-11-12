// Package discovery answers LAN broadcast probes from the companion app. It is
// deliberately dependency free: one UDP listener, one JSON request, one JSON
// reply, sent unicast back to the asker.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// DefaultPort is the UDP port the responder listens on.
const DefaultPort = 8099

const (
	probeName  = "discover"
	serverName = "server"
	protocol   = 1
	// replyEvery bounds how often one source address gets an answer.
	replyEvery = time.Second
)

// Info is the advertisement sent to a probing client.
type Info struct {
	ID            string
	Name          string
	Version       string
	SetupRequired bool
}

// Options configures the responder.
type Options struct {
	// Port is the UDP port; 0 asks the kernel for a free one (tests).
	Port int
	// HTTPPort is the port of the web UI, used to build the api URL.
	HTTPPort int
	// Info is evaluated per reply so a renamed site is picked up immediately.
	Info func() Info
	Log  *slog.Logger
	Now  func() time.Time
}

// Responder listens for probes and answers them.
type Responder struct {
	conn *net.UDPConn
	opts Options
	log  *slog.Logger
	now  func() time.Time

	mu     sync.Mutex
	replied map[netip.Addr]time.Time
}

type probe struct {
	Aiptimize string `json:"aiptimize"`
	V         int    `json:"v"`
}

type reply struct {
	Aiptimize     string `json:"aiptimize"`
	V             int    `json:"v"`
	InstanceID    string `json:"instance_id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	API           string `json:"api"`
	SetupRequired bool   `json:"setup_required"`
}

// New binds the UDP socket. Call Run to serve.
func New(opts Options) (*Responder, error) {
	if opts.Info == nil {
		return nil, errors.New("discovery: Info is required")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Port < 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("discovery: invalid port %d", opts.Port)
	}
	port := opts.Port
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port, IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	return &Responder{
		conn:    conn,
		opts:    opts,
		log:     opts.Log,
		now:     opts.Now,
		replied: make(map[netip.Addr]time.Time),
	}, nil
}

// Addr is the bound UDP address.
func (r *Responder) Addr() net.Addr { return r.conn.LocalAddr() }

// Close stops the listener.
func (r *Responder) Close() error { return r.conn.Close() }

// Run answers probes until the context is cancelled.
func (r *Responder) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = r.conn.Close()
	}()

	buf := make([]byte, 512)
	for {
		n, src, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		r.handle(buf[:n], src)
	}
}

func (r *Responder) handle(payload []byte, src *net.UDPAddr) {
	var p probe
	if err := json.Unmarshal(payload, &p); err != nil || p.Aiptimize != probeName || p.V != protocol {
		return
	}
	if !r.allowReply(src.AddrPort().Addr()) {
		return
	}
	info := r.opts.Info()
	api := r.apiURL(src)
	if api == "" {
		return
	}
	body, err := json.Marshal(reply{
		Aiptimize:     serverName,
		V:             protocol,
		InstanceID:    info.ID,
		Name:          info.Name,
		Version:       info.Version,
		API:           api,
		SetupRequired: info.SetupRequired,
	})
	if err != nil {
		return
	}
	if _, err := r.conn.WriteToUDP(body, src); err != nil {
		r.log.Debug("discovery reply failed", "err", err)
		return
	}
	r.log.Debug("discovery reply sent", "to", src.String(), "api", api)
}

// allowReply rate limits one reply per source address.
func (r *Responder) allowReply(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.replied[addr]; ok && now.Sub(last) < replyEvery {
		return false
	}
	// Prune stale entries so the map cannot grow without bound.
	if len(r.replied) > 256 {
		for key, seen := range r.replied {
			if now.Sub(seen) > time.Minute {
				delete(r.replied, key)
			}
		}
	}
	r.replied[addr] = now
	return true
}

// apiURL builds the URL a client on the sender's network can reach us on.
func (r *Responder) apiURL(dst *net.UDPAddr) string {
	conn, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		return ""
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil || local.IP.IsUnspecified() {
		return ""
	}
	return "http://" + net.JoinHostPort(local.IP.String(), strconv.Itoa(r.opts.HTTPPort))
}
