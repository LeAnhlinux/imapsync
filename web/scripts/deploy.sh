#!/bin/bash
set -e

#############################################################
#  imapsync Web UI - Remote Deploy Script
#  Chạy từ local, tự SSH cài lên remote server (Debian 12)
#
#  Usage:
#    ./deploy.sh -i <SERVER_IP> -d <DOMAIN> [-e EMAIL] [-u USER] [-p SSH_PORT]
#
#  Examples:
#    ./deploy.sh -i 163.227.116.23 -d sync.wppanel.io.vn
#    ./deploy.sh -i 10.0.0.5 -d mail.example.com -e admin@example.com -u root -p 22
#############################################################

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

print_step() { echo -e "\n${CYAN}[$1/$TOTAL_STEPS]${NC} ${GREEN}$2${NC}"; }
print_ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
print_warn() { echo -e "  ${YELLOW}⚠${NC} $1"; }
print_err()  { echo -e "  ${RED}✗${NC} $1"; }

# Defaults
SSH_USER="root"
SSH_PORT="22"
SSL_EMAIL=""
INSTALL_DIR="/opt/imapsync-web"
TOTAL_STEPS=9

# Parse arguments
while getopts "i:d:e:u:p:h" opt; do
    case $opt in
        i) SERVER_IP="$OPTARG" ;;
        d) DOMAIN="$OPTARG" ;;
        e) SSL_EMAIL="$OPTARG" ;;
        u) SSH_USER="$OPTARG" ;;
        p) SSH_PORT="$OPTARG" ;;
        h) echo "Usage: $0 -i <SERVER_IP> -d <DOMAIN> [-e EMAIL] [-u USER] [-p SSH_PORT]"
           echo ""
           echo "Options:"
           echo "  -i  Server IP address (required)"
           echo "  -d  Domain name, e.g. sync.example.com (required)"
           echo "  -e  Email for Let's Encrypt SSL (default: admin@DOMAIN)"
           echo "  -u  SSH user (default: root)"
           echo "  -p  SSH port (default: 22)"
           exit 0 ;;
        *) echo "Use -h for help"; exit 1 ;;
    esac
done

# Validate required args
if [ -z "$SERVER_IP" ] || [ -z "$DOMAIN" ]; then
    echo -e "${RED}Error: -i SERVER_IP and -d DOMAIN are required${NC}"
    echo "Usage: $0 -i <SERVER_IP> -d <DOMAIN> [-e EMAIL] [-u USER] [-p SSH_PORT]"
    exit 1
fi

# Default email from domain
[ -z "$SSL_EMAIL" ] && SSL_EMAIL="admin@${DOMAIN#*.}"

# Resolve script directory (web/ root)
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SSH_CMD="ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -p $SSH_PORT $SSH_USER@$SERVER_IP"
SCP_CMD="rsync -avz -e 'ssh -p $SSH_PORT' --exclude 'node_modules' --exclude '.venv' --exclude '__pycache__' --exclude '*.pyc' --exclude '.git'"

echo ""
echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}  imapsync Web UI - Remote Deploy${NC}"
echo -e "${GREEN}=======================================${NC}"
echo ""
echo "  Server:    $SSH_USER@$SERVER_IP:$SSH_PORT"
echo "  Domain:    $DOMAIN"
echo "  SSL Email: $SSL_EMAIL"
echo "  Install:   $INSTALL_DIR"
echo "  Source:    $SCRIPT_DIR"
echo ""
read -p "Continue? [y/N] " -n 1 -r
echo ""
[[ ! $REPLY =~ ^[Yy]$ ]] && { echo "Aborted."; exit 0; }

#############################################################
print_step 1 "Testing SSH connection"
#############################################################
if ! $SSH_CMD "echo ok" > /dev/null 2>&1; then
    print_err "Cannot SSH to $SSH_USER@$SERVER_IP:$SSH_PORT"
    echo "  Check your SSH key or password authentication."
    exit 1
fi
REMOTE_OS=$($SSH_CMD "cat /etc/debian_version 2>/dev/null || echo unknown")
print_ok "Connected. Remote OS: Debian $REMOTE_OS"

#############################################################
print_step 2 "Uploading files to server"
#############################################################
# Upload web UI code
eval $SCP_CMD "$SCRIPT_DIR/" "$SSH_USER@$SERVER_IP:/tmp/imapsync-web/" 2>&1 | tail -1
print_ok "Web UI code uploaded to /tmp/imapsync-web/"

# Upload imapsync Perl script
if [ -f "$SCRIPT_DIR/../imapsync" ]; then
    scp -P "$SSH_PORT" "$SCRIPT_DIR/../imapsync" "$SSH_USER@$SERVER_IP:/tmp/imapsync" > /dev/null 2>&1
    print_ok "imapsync Perl script uploaded"
fi

#############################################################
print_step 3 "Installing system packages + Perl dependencies"
#############################################################
$SSH_CMD 'bash -s' <<'REMOTE_SCRIPT'
set -e
export DEBIAN_FRONTEND=noninteractive

apt-get update -qq

# Core packages
apt-get install -y -qq \
    curl wget git make openssl ca-certificates gnupg dnsutils \
    > /dev/null 2>&1

# Perl dependencies for imapsync (Debian 12 Bookworm packages)
apt-get install -y -qq \
    libauthen-ntlm-perl \
    libcgi-pm-perl \
    libcrypt-openssl-rsa-perl \
    libdata-uniqid-perl \
    libencode-imaputf7-perl \
    libfile-copy-recursive-perl \
    libfile-tail-perl \
    libio-socket-inet6-perl \
    libio-socket-ssl-perl \
    libio-tee-perl \
    libhtml-parser-perl \
    libjson-webtoken-perl \
    libmail-imapclient-perl \
    libparse-recdescent-perl \
    libproc-processtable-perl \
    libmodule-scandeps-perl \
    libreadonly-perl \
    libregexp-common-perl \
    libsys-meminfo-perl \
    libterm-readkey-perl \
    libtest-mockobject-perl \
    libtest-pod-perl \
    libunicode-string-perl \
    liburi-perl \
    libwww-perl \
    libtest-nowarnings-perl \
    libtest-deep-perl \
    libtest-warn-perl \
    cpanminus \
    > /dev/null 2>&1

echo "DONE_STEP3"
REMOTE_SCRIPT
print_ok "System + Perl packages installed"

#############################################################
print_step 4 "Installing imapsync + Python + Nginx"
#############################################################
$SSH_CMD 'bash -s' <<'REMOTE_SCRIPT'
set -e
export DEBIAN_FRONTEND=noninteractive

# Install imapsync
if [ -f /tmp/imapsync ]; then
    cp /tmp/imapsync /usr/bin/imapsync
    chmod +x /usr/bin/imapsync
fi

# Python 3 + venv
apt-get install -y -qq python3 python3-venv python3-pip > /dev/null 2>&1

# Node.js (Debian 12 has v18)
apt-get install -y -qq nodejs npm > /dev/null 2>&1 || true

# Nginx + Certbot
apt-get install -y -qq nginx certbot python3-certbot-nginx > /dev/null 2>&1

echo "imapsync=$(imapsync --version 2>&1 | head -1 || echo 'N/A')"
echo "python=$(python3 --version)"
echo "node=$(node --version 2>/dev/null || echo 'N/A')"
echo "nginx=$(nginx -v 2>&1)"
echo "DONE_STEP4"
REMOTE_SCRIPT
print_ok "imapsync, Python, Node.js, Nginx installed"

#############################################################
print_step 5 "Setting up Python venv + backend"
#############################################################
$SSH_CMD "bash -s" <<REMOTE_SCRIPT
set -e
INSTALL_DIR="$INSTALL_DIR"

mkdir -p "\$INSTALL_DIR/data/logs"
python3 -m venv "\$INSTALL_DIR/venv"
"\$INSTALL_DIR/venv/bin/pip" install -q --upgrade pip 2>&1 | tail -1
"\$INSTALL_DIR/venv/bin/pip" install -q -r /tmp/imapsync-web/backend/requirements.txt 2>&1 | tail -3

# Copy backend code
cp -r /tmp/imapsync-web/backend/ "\$INSTALL_DIR/backend/"

echo "DONE_STEP5"
REMOTE_SCRIPT
print_ok "Python venv + backend deployed"

#############################################################
print_step 6 "Deploying frontend"
#############################################################
$SSH_CMD "bash -s" <<REMOTE_SCRIPT
set -e
INSTALL_DIR="$INSTALL_DIR"

# Use pre-built dist if available
if [ -d /tmp/imapsync-web/frontend/dist ]; then
    mkdir -p "\$INSTALL_DIR/static"
    cp -r /tmp/imapsync-web/frontend/dist/* "\$INSTALL_DIR/static/"
    echo "PRE_BUILT"
else
    # Build on server
    cd /tmp/imapsync-web/frontend
    npm ci --silent 2>/dev/null
    npm run build --silent 2>/dev/null
    mkdir -p "\$INSTALL_DIR/static"
    cp -r dist/* "\$INSTALL_DIR/static/"
    echo "BUILT_ON_SERVER"
fi
echo "DONE_STEP6"
REMOTE_SCRIPT
print_ok "Frontend deployed"

#############################################################
print_step 7 "Configuring application (.env, user, permissions)"
#############################################################
$SSH_CMD "bash -s" <<REMOTE_SCRIPT
set -e
INSTALL_DIR="$INSTALL_DIR"
DOMAIN="$DOMAIN"

# Generate .env if not exists
if [ ! -f "\$INSTALL_DIR/.env" ]; then
    FERNET_KEY=\$("\$INSTALL_DIR/venv/bin/python3" -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())")
    SECRET_KEY=\$(openssl rand -hex 32)
    IMAPSYNC_PATH=\$(which imapsync 2>/dev/null || echo "/usr/bin/imapsync")

    cat > "\$INSTALL_DIR/.env" <<EOF
SECRET_KEY=\$SECRET_KEY
IMAPSYNC_FERNET_KEY=\$FERNET_KEY
DATABASE_URL=sqlite:///\$INSTALL_DIR/data/imapsync.db
IMAPSYNC_PATH=\$IMAPSYNC_PATH
DOMAIN=\$DOMAIN
EOF
    chmod 600 "\$INSTALL_DIR/.env"
    echo "ENV_CREATED"
else
    echo "ENV_EXISTS"
fi

# Create system user
useradd -r -s /bin/false imapsync 2>/dev/null || true
chown -R imapsync:imapsync "\$INSTALL_DIR"
echo "DONE_STEP7"
REMOTE_SCRIPT
print_ok "Application configured"

#############################################################
print_step 8 "Configuring systemd + Nginx"
#############################################################
$SSH_CMD "bash -s" <<REMOTE_SCRIPT
set -e
INSTALL_DIR="$INSTALL_DIR"
DOMAIN="$DOMAIN"

# Systemd service
cat > /etc/systemd/system/imapsync-web.service <<EOF
[Unit]
Description=imapsync Web UI
After=network.target

[Service]
Type=simple
User=imapsync
WorkingDirectory=\$INSTALL_DIR
EnvironmentFile=\$INSTALL_DIR/.env
ExecStart=\$INSTALL_DIR/venv/bin/uvicorn backend.app.main:app --host 127.0.0.1 --port 8000 --workers 2
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable imapsync-web > /dev/null 2>&1
systemctl restart imapsync-web

# Nginx config
cat > /etc/nginx/sites-available/imapsync-web <<EOF
server {
    listen 80;
    server_name \$DOMAIN;

    root \$INSTALL_DIR/static;
    index index.html;

    location /api/ {
        proxy_pass http://127.0.0.1:8000;
        proxy_set_header Host \\\$host;
        proxy_set_header X-Real-IP \\\$remote_addr;
        proxy_set_header X-Forwarded-For \\\$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \\\$scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \\\$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }

    location / {
        try_files \\\$uri \\\$uri/ /index.html;
    }

    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";
    client_max_body_size 50M;
}
EOF

ln -sf /etc/nginx/sites-available/imapsync-web /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default

nginx -t 2>&1
systemctl reload nginx
echo "DONE_STEP8"
REMOTE_SCRIPT
print_ok "systemd + Nginx configured"

#############################################################
print_step 9 "Setting up SSL (Let's Encrypt)"
#############################################################
SSL_RESULT=$($SSH_CMD "certbot --nginx -d $DOMAIN --non-interactive --agree-tos -m $SSL_EMAIL --redirect 2>&1" || true)
if echo "$SSL_RESULT" | grep -q "Successfully"; then
    print_ok "SSL certificate installed for $DOMAIN"
else
    print_warn "Certbot failed (domain may not point to this server yet)"
    echo "  Run later on server: certbot --nginx -d $DOMAIN"
fi

# Final health check
sleep 2
HEALTH=$($SSH_CMD "curl -sk -o /dev/null -w '%{http_code}' https://$DOMAIN/ 2>/dev/null || curl -s -o /dev/null -w '%{http_code}' http://$DOMAIN/ 2>/dev/null || echo 'N/A'")

echo ""
echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}  Deploy Complete!${NC}"
echo -e "${GREEN}=======================================${NC}"
echo ""
echo -e "  URL:        ${CYAN}https://$DOMAIN${NC}"
echo -e "  Status:     HTTP $HEALTH"
echo ""
echo "  Server commands:"
echo "    systemctl status imapsync-web     # Service status"
echo "    systemctl restart imapsync-web    # Restart backend"
echo "    journalctl -u imapsync-web -f     # View logs"
echo "    nginx -t && systemctl reload nginx"
echo "    certbot renew                     # Renew SSL"
echo ""
echo "  Open https://$DOMAIN and register the first user."
echo "  (First user becomes admin automatically)"
echo ""

# Cleanup
$SSH_CMD "rm -rf /tmp/imapsync-web /tmp/imapsync" > /dev/null 2>&1 || true
