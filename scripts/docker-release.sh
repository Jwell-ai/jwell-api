#!/bin/bash
set -euo pipefail

REGISTRY="registry.digitalocean.com/alphart/alphart"
IMAGE_NAME="jwell-api"
VERSION_FILE="$(dirname "$0")/../VERSION"

BUMP="${1:-patch}"

# Get current version from registry, fall back to VERSION file
echo "Fetching version from registry..."
current_version=$(doctl registry repository list-tags alphart --no-header --format Tag 2>/dev/null \
    | awk '{print $1}' \
    | grep "^${IMAGE_NAME}-[0-9]*\.[0-9]*\.[0-9]*$" \
    | sed "s/^${IMAGE_NAME}-//" \
    | sort -V | tail -1 || true)

if [ -z "$current_version" ]; then
    echo "No version found in registry, reading from VERSION file..."
    current_version=$(cat "$VERSION_FILE" | tr -d '[:space:]' | sed 's/^v//')
fi

# Bump version
major=$(echo "$current_version" | cut -d. -f1)
minor=$(echo "$current_version" | cut -d. -f2)
patch=$(echo "$current_version" | cut -d. -f3)

case "$BUMP" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
  *) echo "Usage: $0 [patch|minor|major]"; exit 1 ;;
esac

new_version="${major}.${minor}.${patch}"
TAG_VERSION="${REGISTRY}:${IMAGE_NAME}-${new_version}"
TAG_LATEST="${REGISTRY}:${IMAGE_NAME}-latest"

echo "Version: ${current_version} -> ${new_version}"

cd "$(dirname "$0")/.."

if [ ! -d "web/dist" ] || [ -z "$(ls -A web/dist 2>/dev/null)" ]; then
    echo "Building frontend (web/dist not found)..."
    cd web && bun install && \
        DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="${new_version}" bun run build
    cd ..
fi

echo "Building binary..."
GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags embed_frontend \
    -ldflags "-s -w -extldflags '-static' -X 'github.com/Jwell-ai/jwell-api/common.Version=${new_version}'" \
    -o new-api

echo "Building ${TAG_VERSION}..."
sudo -E docker build --platform linux/amd64 \
    -t "$TAG_VERSION" -t "$TAG_LATEST" .

echo "Pushing ${TAG_VERSION}..."
sudo -E docker push "$TAG_VERSION"
sudo -E docker push "$TAG_LATEST"

echo "Done: ${TAG_VERSION}"
