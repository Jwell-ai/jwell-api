#!/bin/bash

# Alphart Book Web - Build and Deploy Script
# Usage: ./deploy.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
# Load sensitive config from environment variables (set in shell profile or pass inline)
SERVER_USER="${DEPLOY_USER:-root}"
SERVER_HOST="${DEPLOY_HOST:-192.241.170.213}"
SERVER_PASSWORD="${DEPLOY_PASSWORD:-peanut2026@KINDLEWOOD}"
REMOTE_PATH="${DEPLOY_PATH:-/var/www/api.easycase.vip/}"
WEBSITE_URL="${DEPLOY_WEBSITE_URL:-https://www.alphart.ai}"

if [ -z "$SERVER_HOST" ]; then
    echo -e "${RED}Error: DEPLOY_HOST environment variable is not set.${NC}"
    echo -e "${YELLOW}Please set DEPLOY_HOST, DEPLOY_USER, DEPLOY_PASSWORD, and DEPLOY_PATH before running this script.${NC}"
    exit 1
fi

if [ -z "$SERVER_PASSWORD" ]; then
    echo -e "${RED}Error: DEPLOY_PASSWORD environment variable is not set.${NC}"
    echo -e "${YELLOW}Example:${NC}"
    echo -e "${YELLOW}DEPLOY_HOST=your_digital_ocean_ip DEPLOY_USER=root DEPLOY_PASSWORD='your_password' ./deploy.sh${NC}"
    exit 1
fi

if ! command -v sshpass >/dev/null 2>&1; then
    echo -e "${RED}Error: sshpass is required for password authentication.${NC}"
    echo -e "${YELLOW}Install it first, for example: brew install hudochenkov/sshpass/sshpass${NC}"
    exit 1
fi

export SSHPASS="$SERVER_PASSWORD"
SSH_OPTIONS="-o PreferredAuthentications=password -o PubkeyAuthentication=no -o StrictHostKeyChecking=accept-new"
SSH_CMD="sshpass -e ssh $SSH_OPTIONS"
RSYNC_SSH_CMD="sshpass -e ssh $SSH_OPTIONS"
REMOTE_PATH_ESCAPED=$(printf '%q' "$REMOTE_PATH")
SERVER_PASSWORD_ESCAPED=$(printf '%q' "$SERVER_PASSWORD")

echo -e "${YELLOW}================================${NC}"
echo -e "${YELLOW}  Alphart Book Web Deployment  ${NC}"
echo -e "${YELLOW}================================${NC}"
echo ""

# Build the project
# echo -e "${YELLOW}Step 1: Building project...${NC}"
# npm run build
# echo -e "${GREEN}✓ Build completed${NC}"
# echo ""

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
    -e "$RSYNC_SSH_CMD" \
    dist/ \
    "$SERVER_USER@$SERVER_HOST:$REMOTE_PATH"

echo ""
echo -e "${GREEN}✓ Files deployed${NC}"
echo ""

# Fix permissions on server
echo -e "${YELLOW}Step 3: Fixing permissions...${NC}"
$SSH_CMD "$SERVER_USER@$SERVER_HOST" "REMOTE_PATH=$REMOTE_PATH_ESCAPED SERVER_PASSWORD=$SERVER_PASSWORD_ESCAPED bash -s" << 'EOF'
    run_sudo() {
        if [ "$(id -u)" -eq 0 ]; then
            "$@"
        else
            echo "$SERVER_PASSWORD" | sudo -S "$@"
        fi
    }

    echo "Setting ownership to www-data..."
    run_sudo chown -R www-data:www-data "$REMOTE_PATH"
    
    echo "Setting permissions..."
    run_sudo chmod -R 755 "$REMOTE_PATH"
    
    echo "Checking index.html..."
    if [ -f "$REMOTE_PATH/index.html" ]; then
        echo "✓ index.html exists"
    else
        echo "✗ index.html not found!"
        exit 1
    fi
    
    echo "Restarting web server..."
    if command -v nginx &> /dev/null; then
        run_sudo systemctl restart nginx
        echo "✓ Nginx restarted"
    elif command -v apache2 &> /dev/null; then
        run_sudo systemctl restart apache2
        echo "✓ Apache restarted"
    else
        echo "! Web server not detected"
    fi
EOF

echo ""
echo -e "${GREEN}✓ Permissions fixed${NC}"
echo ""

# Test deployment
echo -e "${YELLOW}Step 4: Testing deployment...${NC}"
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$WEBSITE_URL/" || echo "000")

if [ "$HTTP_STATUS" = "200" ]; then
    echo -e "${GREEN}✓ Website is accessible (HTTP 200)${NC}"
elif [ "$HTTP_STATUS" = "000" ]; then
    echo -e "${YELLOW}! Could not connect to website (check DNS/SSL)${NC}"
else
    echo -e "${RED}✗ Website returned HTTP $HTTP_STATUS${NC}"
    echo -e "${YELLOW}Checking server logs...${NC}"
    $SSH_CMD "$SERVER_USER@$SERVER_HOST" "SERVER_PASSWORD=$SERVER_PASSWORD_ESCAPED bash -s" << 'EOF'
        run_sudo() {
            if [ "$(id -u)" -eq 0 ]; then
                "$@"
            else
                echo "$SERVER_PASSWORD" | sudo -S "$@"
            fi
        }

        run_sudo tail -20 /var/log/nginx/error.log 2>/dev/null || run_sudo tail -20 /var/log/apache2/error.log 2>/dev/null || echo 'No logs found'
EOF
fi

echo ""
echo -e "${YELLOW}================================${NC}"
echo -e "${GREEN}  Deployment finished!          ${NC}"
echo -e "${YELLOW}================================${NC}"
echo ""
echo -e "${BLUE}Website URL: ${WEBSITE_URL}/${NC}"
