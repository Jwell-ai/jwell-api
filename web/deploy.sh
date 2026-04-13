#!/bin/bash

# Jwell API Web - Build and Deploy Script
# Usage: ./deploy.sh [environment]
# Environment: dev (default) | prod

set -e

# Get script directory
cd "$(dirname "$0")"

# Configuration
ENV=${1:-dev}
PEM_KEY="${HOME}/Documents/pem/ssh2_access_key.pem"
SERVER_USER="ubuntu"
SERVER_HOST="ec2-18-237-255-236.us-west-2.compute.amazonaws.com"
REMOTE_PATH="/var/www/jwell-api/"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${YELLOW}================================${NC}"
echo -e "${YELLOW}  Jwell API Web Deployment  ${NC}"
echo -e "${YELLOW}================================${NC}"
echo ""

# Check if PEM key exists
if [ ! -f "$PEM_KEY" ]; then
    echo -e "${RED}Error: PEM key not found at $PEM_KEY${NC}"
    exit 1
fi

# Build the project
echo -e "${YELLOW}Step 1: Building project...${NC}"
bun run build
echo -e "${GREEN}✓ Build completed${NC}"
echo ""

# Check if dist directory exists
if [ ! -d "dist" ]; then
    echo -e "${RED}Error: dist directory not found.${NC}"
    exit 1
fi

# Deploy files to server
echo -e "${YELLOW}Step 2: Deploying to server...${NC}"
echo "Server: $SERVER_USER@$SERVER_HOST"
echo "Remote path: $REMOTE_PATH"
echo ""

# Use rsync for efficient transfer
rsync -avz --delete \
    -e "ssh -i $PEM_KEY -o StrictHostKeyChecking=no" \
    dist/ \
    "$SERVER_USER@$SERVER_HOST:$REMOTE_PATH"

echo ""
echo -e "${GREEN}✓ Files deployed${NC}"
echo ""



