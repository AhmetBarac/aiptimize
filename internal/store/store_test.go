package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpenAndMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	count, err := st2.UserCount(ctx)
	if err != nil || count != 0 {
		t.Fatalf("UserCount = %d, %v", count, err)
	}
}

func TestUserLifecycle(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	if n, err := st.UserCount(ctx); err != nil || n != 0 {
		t.Fatalf("initial count = %d, %v", n, err)
	}
	u, err := st.CreateUser(ctx, "admin", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 || u.Username != "admin" {
		t.Fatalf("unexpected user: %+v", u)
	}
	got, err := st.UserByUsername(ctx, "admin")
	if err != nil || got.PasswordHash != "hash" || got.ID != u.ID {
		t.Fatalf("UserByUsername = %+v, %v", got, err)
	}
	got2, err := st.UserByID(ctx, u.ID)
	if err != nil || got2.Username != "admin" {
		t.Fatalf("UserByID = %+v, %v", got2, err)
	}
	if _, err := st.UserByUsername(ctx, "nobody"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if n, _ := st.UserCount(ctx); n != 1 {
		t.Fatalf("count = %d", n)
	}
	if err := st.UpdateUserPassword(ctx, u.ID, "newhash"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	got3, _ := st.UserByID(ctx, u.ID)
	if got3.PasswordHash != "newhash" {
		t.Fatalf("hash not updated: %+v", got3)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	ses := Session{ID: "hashid", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeen: now, IP: "127.0.0.1"}
	if err := st.CreateSession(ctx, ses); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := st.SessionByID(ctx, "hashid")
	if err != nil {
		t.Fatalf("SessionByID: %v", err)
	}
	if got.UserID != u.ID || !got.ExpiresAt.Equal(ses.ExpiresAt) {
		t.Fatalf("session mismatch: %+v", got)
	}
	later := now.Add(10 * time.Minute)
	if err := st.TouchSession(ctx, "hashid", later, later.Add(time.Hour)); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	got, _ = st.SessionByID(ctx, "hashid")
	if !got.LastSeen.Equal(later) {
		t.Fatalf("last seen = %v", got.LastSeen)
	}
	if err := st.DeleteSessionsForUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByID(ctx, "hashid"); err != ErrNotFound {
		t.Fatalf("expected deleted session, got %v", err)
	}
}

func TestSettingsAndEvents(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	if _, err := st.GetSetting(ctx, "site"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.SetSetting(ctx, "site", json.RawMessage(`{"name":"Home"}`)); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, "site", json.RawMessage(`{"name":"Cabin"}`)); err != nil {
		t.Fatalf("SetSetting upsert: %v", err)
	}
	raw, err := st.GetSetting(ctx, "site")
	if err != nil || string(raw) != `{"name":"Cabin"}` {
		t.Fatalf("GetSetting = %s, %v", raw, err)
	}
	if err := st.SetSetting(ctx, "bad", json.RawMessage(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	if err := st.InsertEvent(ctx, Event{Severity: "info", Code: "test", Message: "hello", Details: json.RawMessage(`{"a":1}`)}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	events, err := st.ListEvents(ctx, time.Time{}, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("ListEvents = %v, %v", events, err)
	}
	if events[0].Code != "test" || string(events[0].Details) != `{"a":1}` {
		t.Fatalf("unexpected event: %+v", events[0])
	}
	if future := time.Now().Add(time.Minute); true {
		empty, err := st.ListEvents(ctx, future, 10)
		if err != nil || len(empty) != 0 {
			t.Fatalf("expected no events after future since: %v, %v", empty, err)
		}
	}
}

func TestPrune(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	old := time.Now().AddDate(-2, 0, 0)
	if err := st.InsertEvent(ctx, Event{TS: old, Severity: "info", Code: "old", Message: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	events, _ := st.ListEvents(ctx, time.Time{}, 10)
	if len(events) != 0 {
		t.Fatalf("old event should be pruned: %+v", events)
	}
}
