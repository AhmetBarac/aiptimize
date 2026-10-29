package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// PriceStats summarizes the stored points of a delivery range.
type PriceStats struct {
	Points      int
	FetchedAt   time.Time
	Preliminary int
}

// ReplacePrices replaces all stored points for one area/currency within
// [from, to) with the supplied set. This keeps a re-published day consistent
// instead of leaving stale points behind.
func (s *Store) ReplacePrices(
	ctx context.Context,
	area, currency string,
	from, to time.Time,
	points []domain.PricePoint,
	fetchedAt time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM prices WHERE area = ? AND currency = ? AND delivery_start >= ? AND delivery_start < ?`,
		area, currency, formatTime(from), formatTime(to)); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO prices(area, currency, delivery_start, delivery_end, price_mwh, state, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(area, currency, delivery_start) DO UPDATE SET
			delivery_end = excluded.delivery_end,
			price_mwh = excluded.price_mwh,
			state = excluded.state,
			fetched_at = excluded.fetched_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range points {
		if _, err := stmt.ExecContext(ctx,
			area, currency,
			formatTime(p.DeliveryStart), formatTime(p.DeliveryEnd),
			p.PriceMWh, string(p.State), formatTime(fetchedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PricesBetween returns stored points ordered by delivery start.
func (s *Store) PricesBetween(ctx context.Context, area, currency string, from, to time.Time) ([]domain.PricePoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT delivery_start, delivery_end, price_mwh, state
		FROM prices
		WHERE area = ? AND currency = ? AND delivery_start >= ? AND delivery_start < ?
		ORDER BY delivery_start`,
		area, currency, formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PricePoint
	for rows.Next() {
		var start, end, state string
		var priceMWh float64
		if err := rows.Scan(&start, &end, &priceMWh, &state); err != nil {
			return nil, err
		}
		st, err := parseTime(start)
		if err != nil {
			return nil, err
		}
		en, err := parseTime(end)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.PricePoint{
			DeliveryStart: st,
			DeliveryEnd:   en,
			PriceMWh:      priceMWh,
			PriceKWh:      priceMWh / 1000,
			State:         domain.PriceState(state),
		})
	}
	return out, rows.Err()
}

// PriceStatsFor returns the point count, newest fetch time and preliminary
// count for a delivery range.
func (s *Store) PriceStatsFor(ctx context.Context, area, currency string, from, to time.Time) (PriceStats, error) {
	var st PriceStats
	var fetched sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(fetched_at),
		       COALESCE(SUM(CASE WHEN state = 'Preliminary' THEN 1 ELSE 0 END), 0)
		FROM prices
		WHERE area = ? AND currency = ? AND delivery_start >= ? AND delivery_start < ?`,
		area, currency, formatTime(from), formatTime(to)).
		Scan(&st.Points, &fetched, &st.Preliminary)
	if err != nil {
		return PriceStats{}, err
	}
	if fetched.Valid && fetched.String != "" {
		t, err := parseTime(fetched.String)
		if err != nil {
			return PriceStats{}, err
		}
		st.FetchedAt = t
	}
	return st, nil
}

// LatestPriceFetch returns the newest successful fetch time for an area.
func (s *Store) LatestPriceFetch(ctx context.Context, area, currency string) (time.Time, error) {
	var fetched sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(fetched_at) FROM prices WHERE area = ? AND currency = ?`, area, currency).
		Scan(&fetched)
	if err != nil {
		return time.Time{}, err
	}
	if !fetched.Valid || fetched.String == "" {
		return time.Time{}, ErrNotFound
	}
	return parseTime(fetched.String)
}
