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

// LatestPlanByStatus returns the newest plan in one lifecycle state.
func (s *Store) LatestPlanByStatus(ctx context.Context, status domain.PlanStatus) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans WHERE status = ? ORDER BY id DESC LIMIT 1`, status)
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

// LatestManualForDay returns the newest manual draft/applied plan for a day.
func (s *Store) LatestManualForDay(ctx context.Context, day string) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans WHERE locked_day = ? AND origin = 'manual' AND status IN ('draft','applied')
		ORDER BY id DESC LIMIT 1`, day)
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

// LatestDraftForDay returns the newest manual draft for a day.
func (s *Store) LatestDraftForDay(ctx context.Context, day string) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans WHERE locked_day = ? AND origin = 'manual' AND status = 'draft'
		ORDER BY id DESC LIMIT 1`, day)
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

// LatestAppliedForDay returns the newest applied manual plan for a day.
func (s *Store) LatestAppliedForDay(ctx context.Context, day string) (domain.Plan, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
			summary_json, inputs_json, origin, revision, locked_day, note, applied_at,
			applied_by, parent_plan_id, curve_json, warnings_json, no_plan_reason
		FROM plans WHERE locked_day = ? AND origin = 'manual' AND status = 'applied'
		ORDER BY id DESC LIMIT 1`, day)
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

// MarkPlanStatus changes a plan lifecycle status.
func (s *Store) MarkPlanStatus(ctx context.Context, id int64, status domain.PlanStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plans SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkPlanApplied marks a plan revision as applied.
func (s *Store) MarkPlanApplied(ctx context.Context, id int64, actor string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plans SET status = 'applied', applied_at = ?, applied_by = ? WHERE id = ?`,
		formatTime(time.Now()), actor, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SupersedeManualForDay discards every other manual draft/applied plan for a day.
func (s *Store) SupersedeManualForDay(ctx context.Context, day string, exceptID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plans SET status = 'superseded'
		WHERE locked_day = ? AND origin = 'manual' AND status IN ('draft','applied') AND id != ?`,
		day, exceptID)
	return err
}

// SavePlanRevision records an immutable audit row.
func (s *Store) SavePlanRevision(ctx context.Context, plan domain.Plan, actor, action string) error {
	slots, err := json.Marshal(plan.Slots)
	if err != nil {
		return err
	}
	summary, err := json.Marshal(plan.Summary)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO plan_revisions(plan_id, revision, created_at, actor, action, slots_json, summary_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		plan.ID, plan.Revision, formatTime(time.Now()), actor, action, string(slots), string(summary))
	return err
}

// ListPlanRevisions returns revisions for a plan, newest first.
func (s *Store) ListPlanRevisions(ctx context.Context, planID int64) ([]domain.PlanRevision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id, revision, created_at, actor, action, slots_json, summary_json
		FROM plan_revisions WHERE plan_id = ? ORDER BY id DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlanRevision
	for rows.Next() {
		var rev domain.PlanRevision
		var created, slots, summary string
		if err := rows.Scan(&rev.ID, &rev.PlanID, &rev.Revision, &created, &rev.Actor, &rev.Action, &slots, &summary); err != nil {
			return nil, err
		}
		if rev.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(slots), &rev.Slots); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(summary), &rev.Summary); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// RecordScheduleWrite stores one write/readback audit row.
func (s *Store) RecordScheduleWrite(ctx context.Context, write domain.ScheduleWrite) error {
	payload := write.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	readback := write.Readback
	if len(readback) == 0 {
		readback = json.RawMessage(`{}`)
	}
	var planID any
	if write.PlanID != nil {
		planID = *write.PlanID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO schedule_writes(ts_utc, kind, payload_json, response, readback_json, ok, plan_id, plan_revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(write.TS), write.Kind, string(payload), write.Response, string(readback),
		write.OK, planID, write.PlanRevision)
	return err
}

// LatestScheduleWrite returns the newest schedule write.
func (s *Store) LatestScheduleWrite(ctx context.Context) (domain.ScheduleWrite, error) {
	var w domain.ScheduleWrite
	var ts, payload, readback string
	var planID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, ts_utc, kind, payload_json, response, readback_json, ok, plan_id, plan_revision
		FROM schedule_writes ORDER BY id DESC LIMIT 1`).Scan(
		&w.ID, &ts, &w.Kind, &payload, &w.Response, &readback, &w.OK, &planID, &w.PlanRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScheduleWrite{}, ErrNotFound
	}
	if err != nil {
		return domain.ScheduleWrite{}, err
	}
	if w.TS, err = parseTime(ts); err != nil {
		return domain.ScheduleWrite{}, err
	}
	w.Payload = json.RawMessage(payload)
	w.Readback = json.RawMessage(readback)
	if planID.Valid {
		id := planID.Int64
		w.PlanID = &id
	}
	return w, nil
}
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
