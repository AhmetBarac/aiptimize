// Package service contains the background loops that coordinate pollers,
// planners and the schedule reconciler.
package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

// Publisher receives server-sent events.
type Publisher interface {
	Publish(event string, payload any)
}

const (
	todayRefreshInterval  = 6 * time.Hour
	providerRetryInterval = 15 * time.Minute
	tomorrowPublishMinute = 13*60 + 2
)

// PricePoller keeps today and tomorrow prices fresh:
//   - fetch today on startup and every 6 hours,
//   - fetch tomorrow from 13:02 local, retrying every 15 minutes until 23:00,
//   - retry a preliminary tomorrow every 6 hours until it becomes final.
type PricePoller struct {
	svc *prices.Service
	cfg *settings.Service
	log *slog.Logger
	pub Publisher
	now func() time.Time

	mu       sync.Mutex
	attempts map[string]time.Time
	interval time.Duration
}

// NewPricePoller builds the poller.
func NewPricePoller(svc *prices.Service, cfg *settings.Service, log *slog.Logger, pub Publisher) *PricePoller {
	if log == nil {
		log = slog.Default()
	}
	return &PricePoller{
		svc:      svc,
		cfg:      cfg,
		log:      log,
		pub:      pub,
		now:      time.Now,
		attempts: make(map[string]time.Time),
		interval: time.Minute,
	}
}

// Run starts the poll loop until ctx is cancelled.
func (p *PricePoller) Run(ctx context.Context) {
	p.tick(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

// tick performs one scheduling decision.
func (p *PricePoller) tick(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	loc := p.cfg.Location()
	now := p.now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	p.ensureToday(ctx, today, now)
	p.ensureTomorrow(ctx, today, now)
}

func (p *PricePoller) ensureToday(ctx context.Context, today, now time.Time) {
	info, err := p.svc.DayInfo(ctx, today)
	if err != nil {
		p.log.Warn("price day status failed", "err", err)
		return
	}
	needs := info.Points == 0 ||
		info.FetchedAt.IsZero() ||
		now.Sub(info.FetchedAt) > todayRefreshInterval
	if !needs {
		return
	}
	if !p.allowAttempt("today:"+today.Format("2006-01-02"), now, providerRetryInterval) {
		return
	}
	day, err := p.svc.Refresh(ctx, today)
	if err != nil {
		p.logRefreshError("today", today, err)
		return
	}
	p.publish(day)
}

func (p *PricePoller) ensureTomorrow(ctx context.Context, today, now time.Time) {
	if now.Hour()*60+now.Minute() < tomorrowPublishMinute {
		return
	}
	tomorrow := today.AddDate(0, 0, 1)
	info, err := p.svc.DayInfo(ctx, tomorrow)
	if err != nil {
		p.log.Warn("price day status failed", "err", err)
		return
	}
	needs := info.Points == 0 ||
		(info.State == domain.PriceStatePreliminary && now.Sub(info.FetchedAt) > todayRefreshInterval)
	if !needs {
		return
	}
	if !p.allowAttempt("tomorrow:"+tomorrow.Format("2006-01-02"), now, providerRetryInterval) {
		return
	}
	day, err := p.svc.Refresh(ctx, tomorrow)
	if err != nil {
		p.logRefreshError("tomorrow", tomorrow, err)
		return
	}
	p.publish(day)
}

func (p *PricePoller) logRefreshError(label string, day time.Time, err error) {
	switch {
	case errors.Is(err, prices.ErrNotPublished):
		p.log.Info("prices not published yet", "day", label, "date", day.Format("2006-01-02"))
	case errors.Is(err, prices.ErrNoData):
		p.log.Warn("no prices for configured area", "day", label, "date", day.Format("2006-01-02"))
	default:
		p.log.Warn("price refresh failed", "day", label, "date", day.Format("2006-01-02"), "err", err)
	}
}

func (p *PricePoller) publish(day prices.Day) {
	if p.pub == nil {
		return
	}
	p.pub.Publish("prices", map[string]any{
		"date":   day.Date,
		"area":   day.Area,
		"points": len(day.Points),
		"state":  day.State,
	})
}

func (p *PricePoller) allowAttempt(key string, now time.Time, interval time.Duration) bool {
	last, ok := p.attempts[key]
	if ok && now.Sub(last) < interval {
		return false
	}
	p.attempts[key] = now
	return true
}
