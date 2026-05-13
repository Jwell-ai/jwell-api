FRONTEND_DIR = ./web
BACKEND_DIR = .
OUTPUT = new-api
REGISTRY  := registry.digitalocean.com/alphart/alphart
IMAGE_NAME := jwell-api
VERSION   := $(shell cat VERSION | tr -d '[:space:]' | sed 's/^v//')

.PHONY: all build-frontend build-backend build-backend-embedded \
        docker-build docker-push docker-release docker-release-minor docker-release-major

all: build-frontend build-backend-embedded

build-frontend:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$$(cat ../VERSION) bun run build

build-backend:
	@echo "Building backend..."
	@cd $(BACKEND_DIR) && go build -ldflags "-s -w -X 'github.com/Jwell-ai/jwell-api/common.Version=$$(cat VERSION)'" -o $(OUTPUT)

build-backend-embedded:
	@echo "Building backend with embedded frontend..."
	@cd $(BACKEND_DIR) && go build -tags embed_frontend -ldflags "-s -w -X 'github.com/Jwell-ai/jwell-api/common.Version=$$(cat VERSION)'" -o $(OUTPUT)

docker-build:
	@if [ ! -d "web/dist" ] || [ -z "$$(ls -A web/dist 2>/dev/null)" ]; then \
		echo "Building frontend (web/dist not found)..."; \
		cd web && bun install && DISABLE_ESLINT_PLUGIN='true' bun run build; \
	fi
	@GOEXPERIMENT=greenteagc CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -tags embed_frontend \
		-ldflags "-s -w -extldflags '-static' -X 'github.com/Jwell-ai/jwell-api/common.Version=$(VERSION)'" \
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
