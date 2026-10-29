package api

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

const placeholderHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Aiptimize</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1220;color:#e6edf7;display:grid;place-items:center;height:100vh;margin:0}
main{max-width:32rem;padding:2rem}code{background:#1c2a44;padding:.15rem .35rem;border-radius:.25rem}</style>
</head><body><main>
<h1>Aiptimize</h1>
<p>The server is running, but the web UI has not been built into this binary.</p>
<p>Build it with <code>cd web &amp;&amp; npm ci &amp;&amp; npm run build</code>, then rebuild the Go binary.</p>
</main></body></html>`

// handleFallback routes unknown API paths to a JSON 404 and everything else to
// the SPA (which supports client-side routes via history fallback).
func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.handleAPINotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.", nil)
		return
	}
	s.handleStatic(w, r)
}

// handleStatic serves the embedded SPA with history-mode fallback.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.web == nil {
		writePlaceholder(w)
		return
	}
	clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if clean == "" || clean == "." {
		clean = "index.html"
	}

	file := clean
	if st, err := fs.Stat(s.web, clean); err != nil || st.IsDir() {
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		file = "index.html"
	}
	data, err := fs.ReadFile(s.web, file)
	if err != nil {
		writePlaceholder(w)
		return
	}

	h := w.Header()
	switch {
	case strings.HasPrefix(file, "assets/"):
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		h.Set("Cache-Control", "no-cache")
	}
	if path.Ext(file) == ".webmanifest" {
		h.Set("Content-Type", "application/manifest+json")
	} else if ct := mime.TypeByExtension(path.Ext(file)); ct != "" {
		h.Set("Content-Type", ct)
	} else if file == "index.html" {
		h.Set("Content-Type", "text/html; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholderHTML))
}
