package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestNewestTag(t *testing.T) {
	got := newestTag([]string{"dev", "sha-abc1234", "0.12.009", "0.12.010", "0.9.999", "latest"})
	if got != "0.12.010" {
		t.Fatalf("newestTag = %q, want 0.12.010", got)
	}
	if newestTag([]string{"dev", "latest"}) != "" {
		t.Fatal("expected no versioned tag")
	}
}

func TestNewerThan(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"0.12.003", "dev-0.12.002", true},
		{"0.12.002", "dev-0.12.002", false},
		{"0.13.000", "dev-0.12.999", true},
		{"0.12.002", "dev", true},
		{"0.12.002", "dev-0.12.002-dirty", false},
		{"0.12.002", "0.12.002", false},
	}
	for _, tc := range cases {
		if got := newerThan(tc.latest, tc.current); got != tc.want {
			t.Errorf("newerThan(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestParseRepo(t *testing.T) {
	ref, err := parseRepo("docker.io/ahmetbarac/aiptimize")
	if err != nil {
		t.Fatal(err)
	}
	if ref.authHost != "auth.docker.io" || ref.apiHost != "registry-1.docker.io" || ref.repo != "ahmetbarac/aiptimize" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	if _, err := parseRepo("nonsense"); err == nil {
		t.Fatal("expected an error for a bare name")
	}
}

func TestCheckAgainstRegistry(t *testing.T) {
	const (
		platformDigest = "sha256:platform"
		configDigest   = "sha256:config"
	)
	// Serve a miniature registry: token, tag list, index, manifest and config.
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "ahmetbarac" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "registry-token"})
	})
	mux.HandleFunc("/v2/ahmetbarac/aiptimize/tags/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer registry-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"name": "ahmetbarac/aiptimize",
			"tags": []string{"dev", "sha-abc1234", "0.12.001", "0.12.002"},
		})
	})
	mux.HandleFunc("/v2/ahmetbarac/aiptimize/manifests/0.12.002", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		json.NewEncoder(w).Encode(map[string]any{
			"manifests": []map[string]any{
				{"digest": platformDigest, "platform": map[string]string{"os": "linux", "architecture": runtime.GOARCH}},
				{"digest": "sha256:attestation", "platform": map[string]string{"os": "unknown", "architecture": "unknown"}},
			},
		})
	})
	mux.HandleFunc("/v2/ahmetbarac/aiptimize/manifests/"+platformDigest, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]string{"digest": configDigest},
		})
	})
	mux.HandleFunc("/v2/ahmetbarac/aiptimize/blobs/"+configDigest, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]any{
				"Labels": map[string]string{
					"org.opencontainers.image.version":  "0.12.002",
					"org.opencontainers.image.revision": "e71668f",
					"org.opencontainers.image.created":  "2026-11-11T23:25:54Z",
					"com.aiptimize.notes":               "Add update check",
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := New("docker.io/ahmetbarac/aiptimize", "ahmetbarac", "secret")
	checker.APIBase = srv.URL
	checker.AuthBase = srv.URL
	checker.Client = srv.Client()

	status, err := checker.Check(context.Background(), "dev-0.12.001")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.Latest != "0.12.002" {
		t.Fatalf("status = %+v", status)
	}
	if status.Notes != "Add update check" || status.Revision != "e71668f" {
		t.Fatalf("metadata = %+v", status)
	}
	if status.Created == nil || status.Created.UTC() != time.Date(2026, 11, 11, 23, 25, 54, 0, time.UTC) {
		t.Fatalf("created = %v", status.Created)
	}
	if status.Digest != platformDigest {
		t.Fatalf("digest = %q", status.Digest)
	}

	if _, err := checker.Check(context.Background(), "dev-0.12.002"); err != nil {
		t.Fatal(err)
	}
}
