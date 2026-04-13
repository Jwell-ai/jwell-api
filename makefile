FRONTEND_DIR = ./web
BACKEND_DIR = .
OUTPUT = new-api

.PHONY: all build-frontend build-backend build-backend-embedded

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
