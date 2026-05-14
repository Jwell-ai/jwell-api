FRONTEND_DEFAULT_DIR = ./web/default
FRONTEND_CLASSIC_DIR = ./web/classic
OUTPUT = new-api
REGISTRY  := registry.digitalocean.com/alphart/alphart
IMAGE_NAME := jwell-api
VERSION   := $(shell cat VERSION | tr -d '[:space:]' | sed 's/^v//')
MODULE    := github.com/QuantumNous/new-api

.PHONY: all \
        build-frontend build-frontend-default build-frontend-classic \
        build-backend build-backend-embedded \
        docker-build docker-push docker-release docker-release-minor docker-release-major

# ── Frontend ──────────────────────────────────────────────────────────────────
build-frontend-default:
	@echo "Building default frontend..."
	@cd $(FRONTEND_DEFAULT_DIR) && bun install && VITE_REACT_APP_VERSION=$$(cat ../../VERSION) bun run build

build-frontend-classic:
	@echo "Building classic frontend..."
	@cd $(FRONTEND_CLASSIC_DIR) && bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$$(cat ../../VERSION) bun run build

build-frontend: build-frontend-default build-frontend-classic

# ── Backend ───────────────────────────────────────────────────────────────────
# build-backend: standalone binary, frontend served separately (nginx/CDN)
build-backend:
	@echo "Building backend (no embedded frontend)..."
	@CGO_ENABLED=0 go build \
		-ldflags "-s -w -X '$(MODULE)/common.Version=$$(cat VERSION)'" \
		-o $(OUTPUT)

# build-backend-embedded: binary with both frontends baked in
build-backend-embedded:
	@echo "Building backend with embedded frontend..."
	@CGO_ENABLED=0 go build -tags embed_frontend \
		-ldflags "-s -w -X '$(MODULE)/common.Version=$$(cat VERSION)'" \
		-o $(OUTPUT)

all: build-frontend build-backend-embedded

# ── Docker ────────────────────────────────────────────────────────────────────
# docker-build assumes frontend is already built (run build-frontend first if needed)
docker-build:
	@echo "Building Go binary for linux/amd64..."
	@GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -tags embed_frontend \
		-ldflags "-s -w -extldflags '-static' -X '$(MODULE)/common.Version=$(VERSION)'" \
		-o $(OUTPUT)
	@sudo -E docker build --platform linux/amd64 \
		-t $(REGISTRY):$(IMAGE_NAME)-$(VERSION) \
		-t $(REGISTRY):$(IMAGE_NAME)-latest \
		.
	@echo "Built $(REGISTRY):$(IMAGE_NAME)-$(VERSION)"

docker-push:
	@sudo -E docker push $(REGISTRY):$(IMAGE_NAME)-$(VERSION)
	@sudo -E docker push $(REGISTRY):$(IMAGE_NAME)-latest
	@echo "Pushed $(REGISTRY):$(IMAGE_NAME)-$(VERSION)"

docker-release:
	@chmod +x scripts/docker-release.sh
	@./scripts/docker-release.sh patch

docker-release-minor:
	@chmod +x scripts/docker-release.sh
	@./scripts/docker-release.sh minor

docker-release-major:
	@chmod +x scripts/docker-release.sh
	@./scripts/docker-release.sh major
