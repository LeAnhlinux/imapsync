#!/bin/bash
set -e

echo "======================================="
echo "  imapsync Web UI - Installer"
echo "  Target: Debian 12 (Bookworm)"
echo "======================================="
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Error: This script must be run as root (sudo bash install.sh)"
    exit 1
fi

# Check Debian version
if [ -f /etc/debian_version ]; then
    DEB_VER=$(cat /etc/debian_version | cut -d. -f1)
    echo "Detected Debian version: $(cat /etc/debian_version)"
else
    echo "Warning: Not a Debian system. This script is designed for Debian 12."
    read -p "Continue anyway? [y/N] " -n 1 -r
    echo ""
    [[ ! $REPLY =~ ^[Yy]$ ]] && exit 0
fi

# Prompt for domain and email
echo ""
read -p "Enter your domain (e.g. sync.example.com): " DOMAIN
read -p "Enter email for SSL cert (Let's Encrypt): " SSL_EMAIL

if [ -z "$DOMAIN" ] || [ -z "$SSL_EMAIL" ]; then
    echo "Error: Domain and email are required."
    exit 1
fi

INSTALL_DIR="/opt/imapsync-web"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo ""
echo "Configuration:"
echo "  Domain:      $DOMAIN"
echo "  Email:       $SSL_EMAIL"
echo "  Install dir: $INSTALL_DIR"
echo "  Source dir:   $SCRIPT_DIR"
echo ""
read -p "Continue? [y/N] " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

echo ""
echo "=========================================="
echo "  Step 1/11: Installing system packages"
echo "=========================================="
apt-get update -qq

# Core system packages
apt-get install -y -qq \
    curl \
    wget \
    git \
    make \
    time \
    openssl \
    ca-certificates \
    gnupg \
    > /dev/null

echo ""
echo "=========================================="
echo "  Step 2/11: Installing imapsync Perl dependencies"
echo "=========================================="
# All Perl modules needed by imapsync on Debian 12 Bookworm
# Source: INSTALL.d/INSTALL.Debian.txt
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
    > /dev/null

echo "  Perl dependencies installed."

echo ""
echo "=========================================="
echo "  Step 3/11: Installing imapsync"
echo "=========================================="
# Check if imapsync is already installed
if command -v imapsync &> /dev/null; then
    CURRENT_VER=$(imapsync --version 2>/dev/null || echo "unknown")
    echo "  imapsync already installed: $CURRENT_VER"
    read -p "  Reinstall/update from this repo? [y/N] " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        cp "$SCRIPT_DIR/../imapsync" /usr/bin/imapsync
        chmod +x /usr/bin/imapsync
        echo "  Updated imapsync from repo."
    fi
else
    # Install from this repo (the imapsync script is in parent dir)
    if [ -f "$SCRIPT_DIR/../imapsync" ]; then
        cp "$SCRIPT_DIR/../imapsync" /usr/bin/imapsync
        chmod +x /usr/bin/imapsync
        echo "  Installed imapsync from repo to /usr/bin/imapsync"
    else
        # Download latest from GitHub
        echo "  Downloading imapsync from GitHub..."
        wget -q -O /usr/bin/imapsync https://raw.githubusercontent.com/imapsync/imapsync/master/imapsync
        chmod +x /usr/bin/imapsync
        echo "  Downloaded and installed imapsync."
    fi
fi

# Verify imapsync works
echo "  Verifying imapsync..."
if imapsync --version > /dev/null 2>&1; then
    echo "  imapsync $(imapsync --version 2>&1 | head -1) OK"
else
    echo "  Warning: imapsync installed but may have missing Perl modules."
    echo "  Run 'imapsync --testslive' after install to check."
fi

echo ""
echo "=========================================="
echo "  Step 4/11: Installing Python 3 + venv"
echo "=========================================="
apt-get install -y -qq python3 python3-venv python3-pip > /dev/null
PYTHON_VER=$(python3 --version)
echo "  $PYTHON_VER"

echo ""
echo "=========================================="
echo "  Step 5/11: Installing Node.js (for frontend build)"
echo "=========================================="
# Debian 12 has Node.js 18 in repos which is sufficient for Vite
# But if user needs newer, we check version first
NODE_VER=$(node --version 2>/dev/null || echo "none")
if [ "$NODE_VER" = "none" ]; then
    # Install from Debian repos (Node 18 on Bookworm)
    apt-get install -y -qq nodejs npm > /dev/null
    NODE_VER=$(node --version)
fi
echo "  Node.js $NODE_VER"

# Check if Node version is >= 18 (required by Vite)
NODE_MAJOR=$(echo "$NODE_VER" | sed 's/v//' | cut -d. -f1)
if [ "$NODE_MAJOR" -lt 18 ]; then
    echo "  Warning: Node.js >= 18 required. Installing from NodeSource..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - > /dev/null 2>&1
    apt-get install -y -qq nodejs > /dev/null
    echo "  Node.js $(node --version) installed from NodeSource."
fi

echo ""
echo "=========================================="
echo "  Step 6/11: Installing Nginx + Certbot"
echo "=========================================="
apt-get install -y -qq nginx certbot python3-certbot-nginx > /dev/null
echo "  Nginx $(nginx -v 2>&1 | cut -d/ -f2) installed."

echo ""
echo "=========================================="
echo "  Step 7/11: Setting up Python backend"
echo "=========================================="
mkdir -p "$INSTALL_DIR"
python3 -m venv "$INSTALL_DIR/venv"
"$INSTALL_DIR/venv/bin/pip" install -q --upgrade pip
"$INSTALL_DIR/venv/bin/pip" install -q -r "$SCRIPT_DIR/backend/requirements.txt"
echo "  Python venv created, dependencies installed."

echo ""
echo "=========================================="
echo "  Step 8/11: Building frontend"
echo "=========================================="
cd "$SCRIPT_DIR/frontend"
npm ci --silent 2>/dev/null
npm run build --silent 2>/dev/null
mkdir -p "$INSTALL_DIR/static"
cp -r dist/* "$INSTALL_DIR/static/"
cd "$SCRIPT_DIR"
echo "  Frontend built and deployed to $INSTALL_DIR/static/"

echo ""
echo "=========================================="
echo "  Step 9/11: Configuring application"
echo "=========================================="
# Create data directories
mkdir -p "$INSTALL_DIR/data/logs"

# Generate secure keys
FERNET_KEY=$("$INSTALL_DIR/venv/bin/python3" -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())")
SECRET_KEY=$(openssl rand -hex 32)
IMAPSYNC_PATH=$(which imapsync 2>/dev/null || echo "/usr/bin/imapsync")

# Write .env (only if not exists, to preserve existing config on re-install)
if [ -f "$INSTALL_DIR/.env" ]; then
    echo "  Existing .env found, keeping it."
    echo "  (Delete $INSTALL_DIR/.env and re-run to regenerate)"
else
    cat > "$INSTALL_DIR/.env" <<EOF
SECRET_KEY=$SECRET_KEY
IMAPSYNC_FERNET_KEY=$FERNET_KEY
DATABASE_URL=sqlite:///$INSTALL_DIR/data/imapsync.db
IMAPSYNC_PATH=$IMAPSYNC_PATH
DOMAIN=$DOMAIN
EOF
    chmod 600 "$INSTALL_DIR/.env"
    echo "  .env created with auto-generated keys."
fi

# Copy backend code
cp -r "$SCRIPT_DIR/backend/" "$INSTALL_DIR/backend/"
echo "  Backend code deployed."

# Create system user
useradd -r -s /bin/false imapsync 2>/dev/null || true
chown -R imapsync:imapsync "$INSTALL_DIR"
echo "  User 'imapsync' configured."

echo ""
echo "=========================================="
echo "  Step 10/11: Configuring systemd + Nginx"
echo "=========================================="
# Systemd service
sed "s|__INSTALL_DIR__|$INSTALL_DIR|g" "$SCRIPT_DIR/scripts/imapsync-web.service" \
    > /etc/systemd/system/imapsync-web.service
systemctl daemon-reload
systemctl enable imapsync-web
systemctl restart imapsync-web
echo "  systemd service enabled and started."

# Nginx config
sed "s|__DOMAIN__|$DOMAIN|g; s|__INSTALL_DIR__|$INSTALL_DIR|g" \
    "$SCRIPT_DIR/scripts/imapsync-web.nginx" \
    > "/etc/nginx/sites-available/imapsync-web"
ln -sf /etc/nginx/sites-available/imapsync-web /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default

if nginx -t 2>/dev/null; then
    systemctl reload nginx
    echo "  Nginx configured and reloaded."
else
    echo "  Warning: Nginx config test failed. Check manually: nginx -t"
fi

echo ""
echo "=========================================="
echo "  Step 11/11: Setting up SSL (Let's Encrypt)"
echo "=========================================="
# Check if domain resolves to this server (basic check)
echo "  Requesting SSL certificate for $DOMAIN..."
if certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$SSL_EMAIL" --redirect 2>&1; then
    echo "  SSL certificate installed!"
else
    echo ""
    echo "  Warning: Certbot failed. Possible reasons:"
    echo "    - Domain $DOMAIN doesn't point to this server yet"
    echo "    - Port 80 is not reachable from internet"
    echo "  The Web UI will work on http://$DOMAIN"
    echo "  Run later: certbot --nginx -d $DOMAIN"
fi

echo ""
echo "======================================="
echo "  Installation complete!"
echo "======================================="
echo ""
echo "  URL:     https://$DOMAIN"
echo "  Service: systemctl status imapsync-web"
echo "  Logs:    journalctl -u imapsync-web -f"
echo "  Config:  $INSTALL_DIR/.env"
echo ""
echo "  Useful commands:"
echo "    systemctl restart imapsync-web    # Restart backend"
echo "    journalctl -u imapsync-web -f     # View backend logs"
echo "    nginx -t && systemctl reload nginx # Reload nginx"
echo "    certbot renew                      # Renew SSL cert"
echo ""
echo "  Open https://$DOMAIN and register the first user."
echo "  (First user becomes admin automatically)"
echo ""
echo "  imapsync version: $(imapsync --version 2>&1 | head -1 || echo 'check manually')"
echo ""
