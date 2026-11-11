// Package update checks the container registry for a newer image of this
// application and reports the build metadata stored in the image labels.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Status is the result of one registry check.
type Status struct {
	Repo      string     `json:"repo"`
	Current   string     `json:"current"`
	Latest    string     `json:"latest"`
	Available bool       `json:"update_available"`
	Notes     string     `json:"notes,omitempty"`
	Revision  string     `json:"revision,omitempty"`
	Created   *time.Time `json:"created,omitempty"`
	Digest    string     `json:"digest,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

// ErrNotConfigured is returned when no registry repository is configured.
var ErrNotConfigured = errors.New("update: registry repository is not configured")

// Checker queries a Docker Registry v2 compatible endpoint.
type Checker struct {
	Repo     string
	Username string
	Token    string
	Client   *http.Client
	Now      func() time.Time
	// AuthBase and APIBase override the derived endpoints; tests use them.
	AuthBase string
	APIBase  string
}

func (c *Checker) authBase(ref registryRef) string {
	if c.AuthBase != "" {
		return strings.TrimRight(c.AuthBase, "/")
	}
	return "https://" + ref.authHost
}

func (c *Checker) apiBase(ref registryRef) string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return "https://" + ref.apiHost
}

// New builds a checker for a repository reference such as
// docker.io/ahmetbarac/aiptimize.
func New(repo, username, token string) *Checker {
	return &Checker{
		Repo:     strings.TrimSpace(repo),
		Username: username,
		Token:    token,
		Client:   &http.Client{Timeout: 20 * time.Second},
		Now:      time.Now,
	}
}

type registryRef struct {
	authHost string
	apiHost  string
	repo     string
}

var tagPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d{3})$`)

// versionPrefix pulls a version out of a build string such as
// "dev-0.12.002-dirty".
var versionPrefix = regexp.MustCompile(`^(\d+\.\d+\.\d{3})`)

func parseRepo(ref string) (registryRef, error) {
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	switch len(parts) {
	case 3:
		host := parts[0]
		if host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io" {
			return registryRef{authHost: "auth.docker.io", apiHost: "registry-1.docker.io",
				repo: parts[1] + "/" + parts[2]}, nil
		}
		return registryRef{authHost: host, apiHost: host, repo: parts[1] + "/" + parts[2]}, nil
	case 2:
		return registryRef{authHost: "auth.docker.io", apiHost: "registry-1.docker.io",
			repo: parts[0] + "/" + parts[1]}, nil
	default:
		return registryRef{}, fmt.Errorf("update: cannot parse repository %q", ref)
	}
}

// Check returns the newest published build and whether it is newer than current.
func (c *Checker) Check(ctx context.Context, current string) (Status, error) {
	if c.Repo == "" {
		return Status{}, ErrNotConfigured
	}
	ref, err := parseRepo(c.Repo)
	if err != nil {
		return Status{}, err
	}
	status := Status{Repo: c.Repo, Current: current, CheckedAt: c.Now().UTC()}

	token, err := c.pullToken(ctx, ref)
	if err != nil {
		return status, err
	}
	tags, err := c.listTags(ctx, ref, token)
	if err != nil {
		return status, err
	}
	latest := newestTag(tags)
	if latest == "" {
		return status, errors.New("update: no versioned tags published")
	}
	status.Latest = latest
	status.Available = newerThan(latest, current)

	labels, digest, err := c.imageMetadata(ctx, ref, token, latest)
	if err != nil {
		return status, err
	}
	status.Digest = digest
	status.Notes = labels["com.aiptimize.notes"]
	status.Revision = labels["org.opencontainers.image.revision"]
	if created, ok := labels["org.opencontainers.image.created"]; ok {
		if ts, err := time.Parse(time.RFC3339, created); err == nil {
			status.Created = &ts
		}
	}
	return status, nil
}

func (c *Checker) pullToken(ctx context.Context, ref registryRef) (string, error) {
	url := fmt.Sprintf("%s/token?service=%s&scope=repository:%s:pull",
		c.authBase(ref), ref.apiHost, ref.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if c.Username != "" && c.Token != "" {
		req.SetBasicAuth(c.Username, c.Token)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: registry auth returned %s", resp.Status)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}
	return "", errors.New("update: registry auth returned no token")
}

func (c *Checker) listTags(ctx context.Context, ref registryRef, token string) ([]string, error) {
	url := fmt.Sprintf("%s/v2/%s/tags/list?n=1000", c.apiBase(ref), ref.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: tag list returned %s", resp.Status)
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Tags, nil
}

// imageMetadata resolves the platform manifest for tag and reads the image
// config labels.
func (c *Checker) imageMetadata(ctx context.Context, ref registryRef, token, tag string) (map[string]string, string, error) {
	accept := strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", ")

	body, mediaType, err := c.manifest(ctx, ref, token, tag, accept)
	if err != nil {
		return nil, "", err
	}
	digest := ""
	if strings.Contains(mediaType, "index") || strings.Contains(mediaType, "manifest.list") {
		var index struct {
			Manifests []manifestEntry `json:"manifests"`
		}
		if err := json.Unmarshal(body, &index); err != nil {
			return nil, "", err
		}
		digest = pickPlatform(index.Manifests)
		if digest == "" {
			return nil, "", errors.New("update: image has no matching platform manifest")
		}
		body, _, err = c.manifest(ctx, ref, token, digest, accept)
		if err != nil {
			return nil, "", err
		}
	}

	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, "", err
	}
	if manifest.Config.Digest == "" {
		return nil, "", errors.New("update: image manifest has no config")
	}
	labels, err := c.blobLabels(ctx, ref, token, manifest.Config.Digest)
	return labels, digest, err
}

// manifestEntry is one platform inside a multi-arch image index.
type manifestEntry struct {
	Digest   string `json:"digest"`
	Platform struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

func pickPlatform(entries []manifestEntry) string {
	wantArch := runtime.GOARCH
	for _, entry := range entries {
		if entry.Platform.OS == "linux" && entry.Platform.Architecture == wantArch {
			return entry.Digest
		}
	}
	// Fall back to the first real manifest so a foreign-arch build still resolves.
	for _, entry := range entries {
		if entry.Platform.OS == "linux" && entry.Platform.Architecture != "unknown" {
			return entry.Digest
		}
	}
	return ""
}

func (c *Checker) manifest(ctx context.Context, ref registryRef, token, reference, accept string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.apiBase(ref), ref.repo, reference)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("update: manifest %s returned %s", reference, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return body, resp.Header.Get("Content-Type"), err
}

func (c *Checker) blobLabels(ctx context.Context, ref registryRef, token, digest string) (map[string]string, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", c.apiBase(ref), ref.repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: config blob returned %s", resp.Status)
	}
	var payload struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Config.Labels == nil {
		return map[string]string{}, nil
	}
	return payload.Config.Labels, nil
}

// newestTag returns the highest 0.xx.yyy tag.
func newestTag(tags []string) string {
	type parsed struct {
		tag     string
		a, b, c int
	}
	var found []parsed
	for _, tag := range tags {
		m := tagPattern.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		found = append(found, parsed{tag: tag, a: a, b: b, c: c})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].a != found[j].a {
			return found[i].a < found[j].a
		}
		if found[i].b != found[j].b {
			return found[i].b < found[j].b
		}
		return found[i].c < found[j].c
	})
	if len(found) == 0 {
		return ""
	}
	return found[len(found)-1].tag
}

// newerThan compares the published tag with the running version, which may be
// prefixed (dev-0.12.002).
func newerThan(latest, current string) bool {
	cur := strings.TrimPrefix(current, "dev-")
	if m := versionPrefix.FindStringSubmatch(cur); m != nil {
		cur = m[1]
	}
	mLatest := tagPattern.FindStringSubmatch(latest)
	mCurrent := tagPattern.FindStringSubmatch(cur)
	if mLatest == nil {
		return false
	}
	if mCurrent == nil {
		return true // a versioned build is newer than an unversioned one
	}
	for i := 1; i <= 2; i++ {
		a, _ := strconv.Atoi(mLatest[i])
		b, _ := strconv.Atoi(mCurrent[i])
		if a != b {
			return a > b
		}
	}
	a, _ := strconv.Atoi(mLatest[3])
	b, _ := strconv.Atoi(mCurrent[3])
	return a > b
}
