package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// SavePlan inserts a plan and its slots in one transaction.
func (s *Store) SavePlan(ctx context.Context, plan domain.Plan) (domain.Plan, error) {
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now()
	}
	if plan.Status == "" {
		plan.Status = domain.PlanStatusSuggestion
	}
	if plan.Origin == "" {
		plan.Origin = domain.OriginAuto
	}
	if plan.Revision == 0 {
		plan.Revision = 1
	}
	summary, err := json.Marshal(plan.Summary)
	if err != nil {
		return plan, err
	}
	inputs := plan.Inputs
	if len(inputs) == 0 {
		inputs = json.RawMessage(`{}`)
	}
	curve, err := json.Marshal(plan.Curve)
	if err != nil {
		return plan, err
	}
	warnings, err := json.Marshal(plan.Warnings)
	if err != nil {
		return plan, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback()

	var locked any
	if plan.LockedDay != "" {
		locked = plan.LockedDay
	}
	var applied any
	if plan.AppliedAt != nil {
		applied = formatTime(*plan.AppliedAt)
	}
	var parent any
	if plan.ParentPlanID != nil {
		parent = *plan.ParentPlanID
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO plans(created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at, applied_by,
			parent_plan_id, curve_json, warnings_json, no_plan_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(plan.CreatedAt), formatTime(plan.HorizonStart), formatTime(plan.HorizonEnd),
		plan.Timezone, plan.Status, plan.InputsHash, string(summary), string(inputs), plan.Origin,
		plan.Revision, locked, plan.Note, applied, plan.AppliedBy, parent, string(curve),
		string(warnings), plan.NoPlanReason)
	if err != nil {
		return plan, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return plan, err
	}
	for i, slot := range plan.Slots {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO plan_slots(plan_id, idx, day_local, start_utc, end_utc, direction,
				power_w, energy_kwh, avg_import_price, expected_saving, reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, i, slot.DayLocal, formatTime(slot.Start), formatTime(slot.End), slot.Direction,
			slot.PowerW, slot.EnergyKWh, slot.AvgImportPrice, slot.ExpectedSaving, slot.Reason)
		if err != nil {
			return plan, err
		}
	}
	if err := tx.Commit(); err != nil {
		return plan, err
	}
	plan.ID = id
	return plan, nil
}

// LatestPlan returns the newest plan with its slots.
func (s *Store) LatestPlan(ctx context.Context) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans ORDER BY id DESC LIMIT 1`)
	plan, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	if err := s.loadPlanSlots(ctx, &plan); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}

// PlanByID returns a plan and its slots.
func (s *Store) PlanByID(ctx context.Context, id int64) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans WHERE id = ?`, id)
	plan, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	if err := s.loadPlanSlots(ctx, &plan); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}

// LatestPlanInputsHash returns the inputs hash of the newest plan.
func (s *Store) LatestPlanInputsHash(ctx context.Context) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT inputs_hash FROM plans ORDER BY id DESC LIMIT 1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

func (s *Store) loadPlanSlots(ctx context.Context, plan *domain.Plan) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT day_local, start_utc, end_utc, direction, power_w, energy_kwh,
			avg_import_price, expected_saving, reason
		FROM plan_slots WHERE plan_id = ? ORDER BY idx`, plan.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	plan.Slots = []domain.Slot{}
	for rows.Next() {
		var slot domain.Slot
		var start, end string
		if err := rows.Scan(&slot.DayLocal, &start, &end, &slot.Direction, &slot.PowerW,
			&slot.EnergyKWh, &slot.AvgImportPrice, &slot.ExpectedSaving, &slot.Reason); err != nil {
			return err
		}
		slot.Start, err = parseTime(start)
		if err != nil {
			return err
		}
		slot.End, err = parseTime(end)
		if err != nil {
			return err
		}
		plan.Slots = append(plan.Slots, slot)
	}
	return rows.Err()
}

func scanPlan(row rowScanner) (domain.Plan, error) {
	var plan domain.Plan
	var created, start, end, summary, inputs, curve, warnings string
	var locked, applied sql.NullString
	var parent sql.NullInt64
	if err := row.Scan(&plan.ID, &created, &start, &end, &plan.Timezone, &plan.Status,
		&plan.InputsHash, &summary, &inputs, &plan.Origin, &plan.Revision, &locked, &plan.Note,
		&applied, &plan.AppliedBy, &parent, &curve, &warnings, &plan.NoPlanReason); err != nil {
		return domain.Plan{}, err
	}
	var err error
	if plan.CreatedAt, err = parseTime(created); err != nil {
		return domain.Plan{}, err
	}
	if plan.HorizonStart, err = parseTime(start); err != nil {
		return domain.Plan{}, err
	}
	if plan.HorizonEnd, err = parseTime(end); err != nil {
		return domain.Plan{}, err
	}
	if err := json.Unmarshal([]byte(summary), &plan.Summary); err != nil {
		return domain.Plan{}, err
	}
	if inputs != "" {
		plan.Inputs = json.RawMessage(inputs)
	}
	if curve != "" && curve != "null" {
		if err := json.Unmarshal([]byte(curve), &plan.Curve); err != nil {
			return domain.Plan{}, err
		}
	}
	if warnings != "" && warnings != "null" {
		if err := json.Unmarshal([]byte(warnings), &plan.Warnings); err != nil {
			return domain.Plan{}, err
		}
	}
	if locked.Valid {
		plan.LockedDay = locked.String
	}
	if applied.Valid {
		t, err := parseTime(applied.String)
		if err != nil {
			return domain.Plan{}, err
		}
		plan.AppliedAt = &t
	}
	if parent.Valid {
		id := parent.Int64
		plan.ParentPlanID = &id
	}
	return plan, nil
}
