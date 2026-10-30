package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/planner"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Plan editing errors map to API codes.
var (
	ErrDraftInvalid     = errors.New("plan: draft is invalid")
	ErrRevisionConflict = errors.New("plan: revision conflict")
	ErrForeignSchedule  = errors.New("plan: inverter has a foreign schedule")
	ErrActiveSlot       = errors.New("plan: an active slot cannot be changed")
)

// DraftSlot is one edited slot as received from the editor.
type DraftSlot struct {
	Start     time.Time        `json:"start"`
	End       time.Time        `json:"end"`
	Direction domain.Direction `json:"direction"`
	PowerW    float64          `json:"power_w"`
}

// PlanView is the editor-facing bundle: auto suggestion, manual draft/applied
// plan, effective plan, validation and the schedule diff.
type PlanView struct {
	Date           string                `json:"date"`
	Auto           *domain.Plan          `json:"auto,omitempty"`
	Draft          *domain.Plan          `json:"draft,omitempty"`
	Applied        *domain.Plan          `json:"applied,omitempty"`
	Effective      *domain.Plan          `json:"effective,omitempty"`
	Inverter       *domain.Plan          `json:"inverter_plan,omitempty"`
	Locked         bool                  `json:"locked"`
	Validation     planner.Validation    `json:"validation"`
	ScheduleDiff   []string              `json:"schedule_diff"`
	InverterMatch  bool                  `json:"inverter_match"`
	InverterReadOK bool                  `json:"inverter_read_ok"`
	Foreign        bool                  `json:"foreign_schedule"`
	Sync           *domain.PlanSync      `json:"sync,omitempty"`
	LastWrite      *domain.ScheduleWrite `json:"last_write,omitempty"`
	Warnings       []string              `json:"warnings"`
}

// ApplyResult is returned after resubmitting or applying the automatic plan.
type ApplyResult struct {
	Plan     domain.Plan     `json:"plan"`
	Schedule domain.Schedule `json:"schedule"`
	Diff     []string        `json:"schedule_diff"`
	Wrote    bool            `json:"wrote"`
	Message  string          `json:"message"`
	Readback domain.Schedule `json:"readback"`
}

// PlanView builds the editor bundle for a local date (empty = today).
func (s *PlannerService) PlanView(ctx context.Context, day string) (PlanView, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	view := PlanView{Date: day, ScheduleDiff: []string{}, Warnings: []string{}}
	if _, err := time.ParseInLocation("2006-01-02", day, loc); err != nil {
		return PlanView{}, fmt.Errorf("invalid date %q", day)
	}
	if auto, err := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion); err == nil {
		view.Auto = &auto
	}
	// A pending draft and the applied plan coexist: the draft is what the editor
	// shows, the applied plan is what the inverter should still be running.
	if draft, err := s.st.LatestDraftForDay(ctx, day); err == nil {
		view.Draft = &draft
		view.Locked = true
	}
	if applied, err := s.st.LatestAppliedForDay(ctx, day); err == nil {
		view.Applied = &applied
		view.Locked = true
	}
	if view.Applied != nil {
		view.Effective = view.Applied
	} else if view.Auto != nil {
		view.Effective = view.Auto
	} else if appliedAuto, err := s.st.LatestPlanByStatus(ctx, domain.PlanStatusApplied); err == nil &&
		appliedAuto.Origin == domain.OriginAuto {
		view.Applied = &appliedAuto
		view.Effective = &appliedAuto
	}
	if view.Draft != nil {
		if result, validation, err := s.Preview(ctx, view.Draft.Slots); err == nil {
			view.Validation = validation
			_ = result
		}
	}
	current, scheduleErr := s.telemetry.ReadSchedule(ctx)
	if scheduleErr == nil {
		view.InverterReadOK = true
		view.Inverter = s.buildInverterPlan(ctx, day, current)
	} else {
		view.Warnings = append(view.Warnings, "Could not read the inverter schedule: "+scheduleErr.Error())
	}
	if view.Effective != nil {
		values := s.cfg.Get()
		manualForSchedule := view.Applied
		autoForSchedule := view.Auto
		if manualForSchedule != nil && manualForSchedule.Origin != domain.OriginManual {
			manualForSchedule = nil
		}
		if autoForSchedule == nil && view.Applied != nil && view.Applied.Origin == domain.OriginAuto {
			autoForSchedule = view.Applied
		}
		if desired, err := BuildSchedule(values, day, manualForSchedule, autoForSchedule); err == nil && scheduleErr == nil {
			view.ScheduleDiff = ScheduleDiff(desired, current)
			view.InverterMatch = len(view.ScheduleDiff) == 0
			view.Foreign = s.foreignSchedule(ctx, current)
			view.Sync = s.saveSync(ctx, day, "check", view.Effective, view.ScheduleDiff, false)
		}
	}
	if view.Sync == nil {
		if last, err := s.st.LatestPlanSync(ctx, day); err == nil {
			view.Sync = &last
		}
	}
	if last, err := s.st.LatestScheduleWrite(ctx); err == nil {
		view.LastWrite = &last
	}
	return view, nil
}

// Preview validates and simulates an edited slot list against current inputs.
func (s *PlannerService) Preview(ctx context.Context, slots []domain.Slot) (planner.Result, planner.Validation, error) {
	inputs, warnings, err := s.BuildInputs(ctx)
	if err != nil {
		return planner.Result{}, planner.Validation{}, err
	}
	_ = warnings
	result, validation, err := planner.Preview(inputs, slots)
	return result, validation, err
}

// planSyncMinInterval throttles automatic comparisons so that polling the plan
// view does not flood the saved sync history with identical rows.
const planSyncMinInterval = 5 * time.Minute

// saveSync stores one comparison between the effective plan and the inverter
// schedule. Unchanged snapshots younger than planSyncMinInterval reuse the
// stored row unless force is set.
func (s *PlannerService) saveSync(ctx context.Context, day, source string, plan *domain.Plan, diff []string, force bool) *domain.PlanSync {
	snap := domain.PlanSync{
		TS: s.now().UTC(), Day: day, Match: len(diff) == 0,
		Diff: append([]string{}, diff...), Source: source,
	}
	if snap.Diff == nil {
		snap.Diff = []string{}
	}
	if plan != nil {
		id := plan.ID
		snap.PlanID = &id
		snap.PlanRevision = plan.Revision
		snap.Origin = plan.Origin
		snap.Status = plan.Status
	}
	if !force {
		if last, err := s.st.LatestPlanSync(ctx, day); err == nil &&
			last.TS.Add(planSyncMinInterval).After(snap.TS) && samePlanSync(last, snap) {
			// Same result within the throttle window: report it as freshly
			// checked but keep the stored history row.
			snap.ID = last.ID
			return &snap
		}
	}
	saved, err := s.st.RecordPlanSync(ctx, snap)
	if err != nil {
		s.log.Warn("save plan sync failed", "err", err)
		return &snap
	}
	return &saved
}

func samePlanSync(a, b domain.PlanSync) bool {
	if a.Match != b.Match || a.PlanRevision != b.PlanRevision ||
		a.Origin != b.Origin || a.Status != b.Status {
		return false
	}
	if (a.PlanID == nil) != (b.PlanID == nil) {
		return false
	}
	if a.PlanID != nil && *a.PlanID != *b.PlanID {
		return false
	}
	if len(a.Diff) != len(b.Diff) {
		return false
	}
	for i := range a.Diff {
		if a.Diff[i] != b.Diff[i] {
			return false
		}
	}
	return true
}

// SaveDraft validates and stores a manual draft for one local day. baseRevision
// and basePlanID are optimistic guards: when the day already has a manual plan,
// they must describe that plan or the save is rejected as a conflict.
func (s *PlannerService) SaveDraft(ctx context.Context, day string, baseRevision int, basePlanID int64, slots []DraftSlot, note, actor string) (domain.Plan, planner.Validation, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	if _, err := time.ParseInLocation("2006-01-02", day, loc); err != nil {
		return domain.Plan{}, planner.Validation{}, fmt.Errorf("invalid date %q", day)
	}
	domainSlots := make([]domain.Slot, 0, len(slots))
	for _, slot := range slots {
		if slot.Start.In(loc).Format("2006-01-02") != day {
			return domain.Plan{}, planner.Validation{}, fmt.Errorf("every draft slot must be on %s", day)
		}
		domainSlots = append(domainSlots, domain.Slot{
			DayLocal: day, Start: slot.Start.UTC(), End: slot.End.UTC(),
			Direction: slot.Direction, PowerW: math.Round(slot.PowerW),
		})
	}
	sort.Slice(domainSlots, func(i, j int) bool { return domainSlots[i].Start.Before(domainSlots[j].Start) })

	inputs, warnings, err := s.BuildInputs(ctx)
	if err != nil {
		return domain.Plan{}, planner.Validation{}, err
	}
	result, validation, err := planner.Preview(inputs, domainSlots)
	if err != nil {
		return domain.Plan{}, validation, err
	}
	if !validation.Valid() {
		return domain.Plan{}, validation, ErrDraftInvalid
	}

	auto, autoErr := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	manual, manualErr := s.st.LatestManualForDay(ctx, day)
	if manualErr != nil && !errors.Is(manualErr, store.ErrNotFound) {
		return domain.Plan{}, validation, manualErr
	}
	currentRevision := 0
	if manualErr == nil {
		currentRevision = manual.Revision
		// Only a manual plan can be edited concurrently: automatic suggestions
		// keep being regenerated and must not look like another editor.
		if baseRevision != 0 && baseRevision != manual.Revision {
			return domain.Plan{}, validation, ErrRevisionConflict
		}
		if basePlanID != 0 && basePlanID != manual.ID {
			return domain.Plan{}, validation, ErrRevisionConflict
		}
	}
	revision := currentRevision + 1
	if revision < 1 {
		revision = 1
	}
	rawInputs, err := json.Marshal(inputs)
	if err != nil {
		return domain.Plan{}, validation, err
	}
	plan := domain.Plan{
		CreatedAt: s.now().UTC(), HorizonStart: inputs.HorizonStart.UTC(), HorizonEnd: inputs.HorizonEnd.UTC(),
		Timezone: inputs.Timezone, Status: domain.PlanStatusDraft, Origin: domain.OriginManual,
		Revision: revision, LockedDay: day, Note: note, Summary: result.Summary,
		Slots: result.Slots, Curve: result.Curve, Warnings: append(warnings, result.Warnings...),
		Inputs: rawInputs,
	}
	if autoErr == nil {
		plan.ParentPlanID = &auto.ID
	}
	saved, err := s.st.SavePlan(ctx, plan)
	if err != nil {
		return domain.Plan{}, validation, err
	}
	if err := s.st.SavePlanRevision(ctx, saved, actor, "draft"); err != nil {
		return domain.Plan{}, validation, err
	}
	// Older drafts for the same day are superseded; an applied plan survives
	// until the new draft is actually applied.
	if manualErr == nil && manual.Status == domain.PlanStatusDraft && manual.ID != saved.ID {
		_ = s.st.MarkPlanStatus(ctx, manual.ID, domain.PlanStatusSuperseded)
	}
	return saved, validation, nil
}

// PreviewDraft revalidates a stored draft without saving it.
func (s *PlannerService) PreviewDraft(ctx context.Context, id int64) (domain.Plan, planner.Result, planner.Validation, error) {
	plan, err := s.st.PlanByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.Plan{}, planner.Result{}, planner.Validation{}, ErrPlanUnavailable
		}
		return domain.Plan{}, planner.Result{}, planner.Validation{}, err
	}
	if plan.Status != domain.PlanStatusDraft {
		return domain.Plan{}, planner.Result{}, planner.Validation{}, fmt.Errorf("plan %d is not a draft", id)
	}
	result, validation, err := s.Preview(ctx, plan.Slots)
	return plan, result, validation, err
}

// DiscardDraft marks a draft as superseded.
func (s *PlannerService) DiscardDraft(ctx context.Context, id int64) error {
	plan, err := s.st.PlanByID(ctx, id)
	if err != nil {
		return err
	}
	if plan.Status != domain.PlanStatusDraft {
		return fmt.Errorf("plan %d is not a draft", id)
	}
	return s.st.MarkPlanStatus(ctx, id, domain.PlanStatusSuperseded)
}

// ApplyDraft resubmits a validated draft to the inverter.
func (s *PlannerService) ApplyDraft(ctx context.Context, id int64, revision int, actor string) (ApplyResult, error) {
	plan, err := s.st.PlanByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ApplyResult{}, ErrPlanUnavailable
		}
		return ApplyResult{}, err
	}
	if plan.Status != domain.PlanStatusDraft {
		return ApplyResult{}, fmt.Errorf("plan %d is not a draft", id)
	}
	if revision != 0 && plan.Revision != revision {
		return ApplyResult{}, ErrRevisionConflict
	}
	if _, validation, err := s.Preview(ctx, plan.Slots); err != nil {
		return ApplyResult{}, err
	} else if !validation.Valid() {
		return ApplyResult{}, fmt.Errorf("%w: %v", ErrDraftInvalid, validation.Errors)
	}
	now := s.now()
	base, err := s.st.LatestAppliedForDay(ctx, plan.LockedDay)
	if err != nil {
		base, _ = s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	}
	for _, active := range base.Slots {
		if active.DayLocal != plan.LockedDay {
			continue
		}
		if !active.Start.Before(now) || !active.End.After(now) {
			continue
		}
		found := false
		for _, slot := range plan.Slots {
			if slot.Start.Equal(active.Start) && slot.End.Equal(active.End) &&
				slot.Direction == active.Direction && math.Abs(slot.PowerW-active.PowerW) < 1 {
				found = true
				break
			}
		}
		if !found {
			return ApplyResult{}, ErrActiveSlot
		}
	}
	auto, _ := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	values := s.cfg.Get()
	desired, err := BuildSchedule(values, plan.LockedDay, &plan, &auto)
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := s.writeSchedule(ctx, desired, &plan, plan.Revision, "apply", false)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.st.MarkPlanApplied(ctx, plan.ID, actor); err != nil {
		return ApplyResult{}, err
	}
	_ = s.st.SupersedeManualForDay(ctx, plan.LockedDay, plan.ID)
	plan.Status = domain.PlanStatusApplied
	appliedAt := time.Now().UTC()
	plan.AppliedAt = &appliedAt
	plan.AppliedBy = actor
	_ = s.st.SavePlanRevision(ctx, plan, actor, "apply")
	result.Plan = plan
	s.publishPlan(plan)
	s.saveSync(ctx, plan.LockedDay, "apply", &plan, result.Diff, true)
	return result, nil
}

// ApplyAuto applies the latest automatic suggestion and clears the manual lock.
func (s *PlannerService) ApplyAuto(ctx context.Context, day string, actor string) (ApplyResult, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	auto, err := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	if err != nil {
		return ApplyResult{}, ErrPlanUnavailable
	}
	values := s.cfg.Get()
	desired, err := BuildSchedule(values, day, nil, &auto)
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := s.writeSchedule(ctx, desired, &auto, auto.Revision, "apply_auto", false)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := s.st.MarkPlanApplied(ctx, auto.ID, actor); err != nil {
		return ApplyResult{}, err
	}
	_ = s.st.SupersedeManualForDay(ctx, day, 0)
	auto.Status = domain.PlanStatusApplied
	now := time.Now().UTC()
	auto.AppliedAt = &now
	auto.AppliedBy = actor
	_ = s.st.SavePlanRevision(ctx, auto, actor, "apply_auto")
	result.Plan = auto
	s.publishPlan(auto)
	s.saveSync(ctx, day, "apply_auto", &auto, result.Diff, true)
	return result, nil
}

// DownloadPlan writes the effective plan for a local day to the inverter. It is
// the explicit manual action behind the "Download plan" button.
func (s *PlannerService) DownloadPlan(ctx context.Context, day, actor string) (ApplyResult, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	auto, autoErr := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	manual, manualErr := s.st.LatestAppliedForDay(ctx, day)
	values := s.cfg.Get()
	var plan *domain.Plan
	var desired domain.Schedule
	var err error
	if manualErr == nil {
		plan = &manual
		desired, err = BuildSchedule(values, day, &manual, &auto)
	} else if autoErr == nil {
		plan = &auto
		desired, err = BuildSchedule(values, day, nil, &auto)
	} else {
		return ApplyResult{}, ErrPlanUnavailable
	}
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := s.writeSchedule(ctx, desired, plan, plan.Revision, "download", false)
	if err != nil {
		return ApplyResult{}, err
	}
	if plan.Status == domain.PlanStatusSuggestion {
		if err := s.st.MarkPlanApplied(ctx, plan.ID, actor); err != nil {
			return ApplyResult{}, err
		}
		plan.Status = domain.PlanStatusApplied
		now := time.Now().UTC()
		plan.AppliedAt = &now
		plan.AppliedBy = actor
	}
	_ = s.st.SavePlanRevision(ctx, *plan, actor, "download")
	result.Plan = *plan
	s.publishPlan(*plan)
	s.saveSync(ctx, day, "download", plan, result.Diff, true)
	return result, nil
}

// SetManualControl locks the current day to its current plan or releases the
// manual lock. Locking copies the inverter plan when available; it never writes
// to the inverter.
func (s *PlannerService) SetManualControl(ctx context.Context, day string, locked bool, actor string) (PlanView, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	if _, err := time.ParseInLocation("2006-01-02", day, loc); err != nil {
		return PlanView{}, fmt.Errorf("invalid date %q", day)
	}
	if !locked {
		if err := s.st.SupersedeManualForDay(ctx, day, 0); err != nil {
			return PlanView{}, err
		}
		return s.PlanView(ctx, day)
	}
	view, err := s.PlanView(ctx, day)
	if err != nil {
		return PlanView{}, err
	}
	if view.Locked {
		return view, nil
	}
	source := view.Inverter
	if source == nil {
		source = view.Effective
	}
	if source == nil {
		return PlanView{}, ErrPlanUnavailable
	}
	now := time.Now().UTC()
	manual := *source
	manual.ID = 0
	manual.Status = domain.PlanStatusApplied
	manual.Origin = domain.OriginManual
	manual.Revision = 1
	manual.LockedDay = day
	manual.CreatedAt = now
	manual.AppliedAt = &now
	manual.AppliedBy = actor
	manual.Slots = nil
	for _, slot := range source.Slots {
		if slot.DayLocal == day {
			manual.Slots = append(manual.Slots, slot)
		}
	}
	if source.ID > 0 {
		parent := source.ID
		manual.ParentPlanID = &parent
	}
	saved, err := s.st.SavePlan(ctx, manual)
	if err != nil {
		return PlanView{}, err
	}
	_ = s.st.SupersedeManualForDay(ctx, day, saved.ID)
	_ = s.st.SavePlanRevision(ctx, saved, actor, "manual_control_on")
	s.publishPlan(saved)
	return s.PlanView(ctx, day)
}

func (s *PlannerService) writeSchedule(ctx context.Context, desired domain.Schedule, plan *domain.Plan, revision int, kind string, force bool) (ApplyResult, error) {
	current, err := s.telemetry.ReadSchedule(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	if !force && s.foreignSchedule(ctx, current) {
		return ApplyResult{}, ErrForeignSchedule
	}
	if !force {
		setpointOnly := len(ScheduleDiff(current, desired)) == 0 && !ScheduleEqual(current, desired)
		if last, err := s.st.LatestScheduleWrite(ctx); err == nil && time.Since(last.TS) < 60*time.Second &&
			!setpointOnly {
			return ApplyResult{}, errors.New("write rate limit: wait 60 seconds between schedule changes")
		}
	}
	readback, wrote, err := s.telemetry.ApplySchedule(ctx, desired)
	if err != nil {
		return ApplyResult{}, err
	}
	diff := ScheduleDiff(desired, readback)
	payload, _ := json.Marshal(desired)
	readbackJSON, _ := json.Marshal(readback)
	write := domain.ScheduleWrite{
		TS: time.Now().UTC(), Kind: kind, Payload: payload, Response: "readback",
		Readback: readbackJSON, OK: len(diff) == 0, PlanID: nil, PlanRevision: revision,
	}
	if plan != nil {
		write.PlanID = &plan.ID
	}
	if err := s.st.RecordScheduleWrite(ctx, write); err != nil {
		return ApplyResult{}, err
	}
	message := "Schedule written."
	if !wrote {
		message = "Already matches; nothing written."
	}
	if len(diff) > 0 {
		message = "Schedule written; readback differs from the desired payload."
	}
	return ApplyResult{
		Schedule: desired, Readback: readback, Diff: diff, Wrote: wrote, Message: message,
	}, nil
}

func (s *PlannerService) foreignSchedule(ctx context.Context, current domain.Schedule) bool {
	last, err := s.st.LatestScheduleWrite(ctx)
	if err != nil {
		return hasScheduleSlots(current)
	}
	var expected domain.Schedule
	if len(last.Readback) > 0 {
		if err := json.Unmarshal(last.Readback, &expected); err == nil {
			return !ScheduleEqual(expected, current)
		}
	}
	return hasScheduleSlots(current)
}

func hasScheduleSlots(s domain.Schedule) bool {
	for _, slots := range s.Days {
		if len(slots) > 0 {
			return true
		}
	}
	return false
}

// ClearSchedule writes an empty schedule and clears every day.
func (s *PlannerService) ClearSchedule(ctx context.Context) (ApplyResult, error) {
	values := s.cfg.Get()
	empty := domain.Schedule{Days: map[string][]domain.ScheduleSlot{}, PinW: math.Round(values.Plan.ChargeW), PoutW: math.Round(values.Plan.DischargeW)}
	for _, day := range scheduleDayKeys {
		empty.Days[day] = []domain.ScheduleSlot{}
	}
	return s.writeSchedule(ctx, empty, nil, 0, "clear", true)
}

// TakeControl replaces a foreign schedule with the effective plan.
func (s *PlannerService) TakeControl(ctx context.Context, day, actor string) (ApplyResult, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	auto, autoErr := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	manual, manualErr := s.st.LatestAppliedForDay(ctx, day)
	values := s.cfg.Get()
	var desired domain.Schedule
	var err error
	if manualErr == nil {
		desired, err = BuildSchedule(values, day, &manual, &auto)
	} else if autoErr == nil {
		desired, err = BuildSchedule(values, day, nil, &auto)
	} else {
		return ApplyResult{}, ErrPlanUnavailable
	}
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := s.writeSchedule(ctx, desired, nil, 0, "takeover", true)
	if err != nil {
		return ApplyResult{}, err
	}
	result.Message = "Control taken; the schedule was replaced."
	s.saveSync(ctx, day, "takeover", nil, result.Diff, true)
	return result, nil
}

func (s *PlannerService) publishPlan(plan domain.Plan) {
	if s.publish == nil {
		return
	}
	s.publish("plan", map[string]any{
		"id": plan.ID, "status": plan.Status, "revision": plan.Revision,
		"summary": plan.Summary, "slots": plan.Slots,
	})
}

// Revisions returns the audit rows for the latest plan on a day.
func (s *PlannerService) Revisions(ctx context.Context, day string) ([]domain.PlanRevision, error) {
	loc := s.cfg.Location()
	if day == "" {
		day = s.now().In(loc).Format("2006-01-02")
	}
	plan, err := s.st.LatestManualForDay(ctx, day)
	if err != nil {
		plan, err = s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion)
	}
	if err != nil {
		return nil, ErrPlanUnavailable
	}
	return s.st.ListPlanRevisions(ctx, plan.ID)
}
