#!/bin/sh
# Pull the published image and restart aiptimize only when the digest changed.
# Usage: aiptimize-update [tag]   (default tag: dev)
#
# The registry credential is the Docker Hub license stored in the application
# settings (Settings -> Site -> License), so the token is rotated in one place.
# AIPTIMIZE_REGISTRY_TOKEN in /opt/aiptimize/app.env is only a fallback.
set -eu

IMAGE="docker.io/ahmetbarac/aiptimize"
TAG="${1:-dev}"
SERVICE="aiptimize"
VOLUME="aiptimize-v1-data"

if [ -f /opt/aiptimize/app.env ]; then
    # shellcheck disable=SC1091
    . /opt/aiptimize/app.env
fi
REGISTRY_USER="${AIPTIMIZE_REGISTRY_USERNAME:-ahmetbarac}"

digest_of() {
    docker image inspect -f '{{index .RepoDigests 0}}' "$IMAGE:$TAG" 2>/dev/null || echo "none"
}

# Read the license from the application database without locking it for writes.
stored_license() {
    mountpoint=$(docker volume inspect -f '{{.Mountpoint}}' "$VOLUME" 2>/dev/null || true)
    [ -n "$mountpoint" ] && [ -f "$mountpoint/aiptimize.db" ] || return 0
    python3 - "$mountpoint/aiptimize.db" <<'PY' 2>/dev/null || true
import json, sqlite3, sys
con = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
row = con.execute("select value_json from settings where key='site'").fetchone()
print(json.loads(row[0]).get("license", "") if row else "", end="")
PY
}

token=$(stored_license)
if [ -z "$token" ]; then
    token="${AIPTIMIZE_REGISTRY_TOKEN:-}"
fi

if [ -n "$token" ]; then
    if ! printf '%s' "$token" | docker login -u "$REGISTRY_USER" --password-stdin >/dev/null 2>&1; then
        echo "registry login failed for $REGISTRY_USER; check the license in Settings -> Site" >&2
        exit 1
    fi
else
    echo "no registry license configured; trying an anonymous pull" >&2
fi

before=$(digest_of)
echo "pulling $IMAGE:$TAG"
docker pull "$IMAGE:$TAG" >/dev/null
after=$(digest_of)

if [ "$before" = "$after" ]; then
    echo "already up to date: $after"
    exit 0
fi

echo "updating: $before -> $after"
systemctl restart "$SERVICE"

# Wait for the health endpoint, then report what is running.
i=0
while [ "$i" -lt 30 ]; do
    if curl -fsS http://127.0.0.1:8080/healthz >/tmp/aiptimize-health.json 2>/dev/null; then
        cat /tmp/aiptimize-health.json
        echo
        docker inspect -f '{{.Config.Image}} started {{.State.StartedAt}}' aiptimize
        exit 0
    fi
    i=$((i + 1))
    sleep 1
done

echo "health check did not come up after restart; check: journalctl -u aiptimize -n 50" >&2
exit 1
