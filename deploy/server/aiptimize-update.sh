#!/bin/sh
# Pull the dev image and restart aiptimize only when the digest changed.
# Usage: aiptimize-update [tag]   (default tag: dev)
set -eu

IMAGE="docker.io/ahmetbarac/aiptimize"
TAG="${1:-dev}"
SERVICE="aiptimize"

digest_of() {
    docker image inspect -f '{{index .RepoDigests 0}}' "$IMAGE:$TAG" 2>/dev/null || echo "none"
}

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
