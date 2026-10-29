package planner

import (
	"testing"
	"time"
)

func TestLongHorizon(t *testing.T) {
	in := baseInputs()
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, testLoc)
	in.Blocks = make([]Block, 96)
	for i := range in.Blocks {
		p := 0.2
		if i%48 >= 32 {
			p = 1.2
		}
		if i%48 < 8 {
			p = 0.05
		}
		in.Blocks[i] = Block{Start: start.Add(time.Duration(i) * 30 * time.Minute), End: start.Add(time.Duration(i+1) * 30 * time.Minute), ImportPrice: p, ExportPrice: 0.05, LoadW: 800}
	}
	in.HorizonStart = in.Blocks[0].Start
	in.HorizonEnd = in.Blocks[95].End
	in.EndSOCTarget = 15
	started := time.Now()
	res, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	t.Logf("elapsed %v slots %d savings %v", elapsed, len(res.Slots), res.Summary.Savings)
	if elapsed > 500*time.Millisecond {
		t.Errorf("96-block plan took %v; target is under 500 ms", elapsed)
	}
}
