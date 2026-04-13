FRONTEND_DIR = ./web
BACKEND_DIR = .
OUTPUT = new-api

.PHONY: all build-frontend build-backend

all: build-frontend build-backend

build-frontend:
	@echo "Building frontend..."
	@cd $(FRONTEND_DIR) && bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$$(cat ../VERSION) bun run build

build-backend:
	@echo "Building backend..."
	@cd $(BACKEND_DIR) && go build -ldflags "-s -w -X 'github.com/Jwell-ai/jwell-api/common.Version=$$(cat VERSION)'" -o $(OUTPUT)
