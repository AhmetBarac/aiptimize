package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestPlanSyncRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	planID := int64(17)
	ts := time.Date(2026, 10, 30, 6, 30, 0, 0, time.UTC)
	saved, err := st.RecordPlanSync(ctx, domain.PlanSync{
		TS: ts, Day: "2026-10-30", PlanID: &planID, PlanRevision: 3,
		Origin: domain.OriginManual, Status: domain.PlanStatusApplied,
		Match: false, Diff: []string{"Mon: 1 slot(s) desired, 0 in inverter"}, Source: "check",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatal("sync ID not set")
	}

	latest, err := st.LatestPlanSync(ctx, "2026-10-30")
	if err != nil {
		t.Fatal(err)
	}
	if latest.PlanID == nil || *latest.PlanID != planID || latest.PlanRevision != 3 ||
		latest.Origin != domain.OriginManual || latest.Status != domain.PlanStatusApplied ||
		latest.Match || latest.Source != "check" {
		t.Fatalf("latest = %+v", latest)
	}
	if !latest.TS.Equal(ts) {
		t.Errorf("ts = %s, want %s", latest.TS, ts)
	}
	if len(latest.Diff) != 1 || latest.Diff[0] != "Mon: 1 slot(s) desired, 0 in inverter" {
		t.Fatalf("diff = %v", latest.Diff)
	}

	if _, err := st.LatestPlanSync(ctx, "2026-10-31"); err != ErrNotFound {
		t.Fatalf("missing day err = %v, want ErrNotFound", err)
	}

	if _, err := st.RecordPlanSync(ctx, domain.PlanSync{
		TS: ts.Add(time.Minute), Day: "2026-10-30", Match: true, Diff: nil, Source: "apply",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListPlanSyncs(ctx, "2026-10-30", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].Match || rows[1].Match {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Diff == nil {
		t.Error("nil diff should be stored as an empty list")
	}
}
