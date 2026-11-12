package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/AhmetBarac/aiptimize/internal/store"
)

func TestLoadInstancePersistsID(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "instance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	first, err := LoadInstance(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ID()) != 36 {
		t.Fatalf("id %q is not a UUID", first.ID())
	}

	second, err := LoadInstance(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID() != first.ID() {
		t.Fatalf("id changed across loads: %q != %q", second.ID(), first.ID())
	}
}
