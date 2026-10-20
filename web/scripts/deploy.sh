#!/bin/bash
set -euo pipefail

#############################################################
#  imapsync Web UI - Remote Deploy (Go backend)
#
#  Builds a static linux/amd64 binary here, then installs it
#  on the server. The server needs no Go, no Python and no
#  Node: everything that gets compiled is compiled locally.
#
#  Usage:
#    ./deploy.sh -i <SERVER_IP> -d <DOMAIN> [-e EMAIL] [-u USER] [-p SSH_PORT]
#
#  Example:
#    ./deploy.sh -i 203.0.113.10 -d sync.example.com
#############################################################

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
TOTAL_STEPS=8
print_step() { echo -e "\n${CYAN}[$1/$TOTAL_STEPS]${NC} ${GREEN}$2${NC}"; }
print_ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
print_warn() { echo -e "  ${YELLOW}⚠${NC} $1"; }
print_err()  { echo -e "  ${RED}✗${NC} $1"; }

SSH_USER="root"; SSH_PORT="22"; SSL_EMAIL=""; INSTALL_DIR="/opt/imapsync-web"

while getopts "i:d:e:u:p:h" opt; do
    case $opt in
        i) SERVER_IP="$OPTARG" ;;
        d) DOMAIN="$OPTARG" ;;
        e) SSL_EMAIL="$OPTARG" ;;
        u) SSH_USER="$OPTARG" ;;
        p) SSH_PORT="$OPTARG" ;;
        h) echo "Usage: $0 -i <SERVER_IP> -d <DOMAIN> [-e EMAIL] [-u USER] [-p SSH_PORT]"; exit 0 ;;
        *) echo "Use -h for help"; exit 1 ;;
    esac
done

if [ -z "${SERVER_IP:-}" ] || [ -z "${DOMAIN:-}" ]; then
    echo -e "${RED}Error: -i SERVER_IP and -d DOMAIN are required${NC}"
    exit 1
fi
# Strip the leading label only when what remains is still a domain:
# "sync.example.com" -> admin@example.com, but "example.com" must not become
# admin@com, which certbot rejects and which made a silent TLS failure likelier.
if [ -z "$SSL_EMAIL" ]; then
    MAIL_DOMAIN="$DOMAIN"
    case "${DOMAIN#*.}" in
        *.*) MAIL_DOMAIN="${DOMAIN#*.}" ;;
    esac
    SSL_EMAIL="admin@$MAIL_DOMAIN"
fi

WEB_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_DIR="$(cd "$WEB_DIR/.." && pwd)"
SSH_CMD="ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -p $SSH_PORT $SSH_USER@$SERVER_IP"

echo ""
echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}  imapsync Web UI - Deploy (Go)${NC}"
echo -e "${GREEN}=======================================${NC}"
echo "  Server:  $SSH_USER@$SERVER_IP:$SSH_PORT"
echo "  Domain:  $DOMAIN"
echo "  Install: $INSTALL_DIR"
echo ""
read -p "Continue? [y/N] " -n 1 -r; echo ""
[[ ! $REPLY =~ ^[Yy]$ ]] && { echo "Aborted."; exit 0; }

#############################################################
print_step 1 "Building the backend and frontend locally"
#############################################################
command -v go >/dev/null || { print_err "go is not installed"; exit 1; }
command -v npm >/dev/null || { print_err "npm is not installed"; exit 1; }

( cd "$WEB_DIR/backend-go" && make linux >/dev/null )
print_ok "Static linux/amd64 binaries built"

( cd "$WEB_DIR/frontend" && npm ci --silent >/dev/null 2>&1 && npm run build --silent >/dev/null 2>&1 )
print_ok "Frontend built"

# The MCP connector runs on users' own machines, so it ships as downloads
# inside the frontend, listed by the manifest the API Access page reads.
( cd "$WEB_DIR/mcp" && make downloads >/dev/null )
mkdir -p "$WEB_DIR/frontend/dist/downloads/mcp"
cp "$WEB_DIR/mcp/dist/"* "$WEB_DIR/frontend/dist/downloads/mcp/"
print_ok "MCP connector built for download"

#############################################################
print_step 2 "Testing the SSH connection"
#############################################################
$SSH_CMD "echo ok" >/dev/null || { print_err "cannot SSH to $SSH_USER@$SERVER_IP:$SSH_PORT"; exit 1; }
print_ok "Connected"

#############################################################
print_step 3 "Installing imapsync and its Perl dependencies"
#############################################################
# imapsync itself is still Perl; only the web layer is Go.
$SSH_CMD 'bash -s' <<'REMOTE'
set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq curl ca-certificates nginx certbot python3-certbot-nginx >/dev/null 2>&1
apt-get install -y -qq \
    libauthen-ntlm-perl libcgi-pm-perl libcrypt-openssl-rsa-perl libdata-uniqid-perl \
    libencode-imaputf7-perl libfile-copy-recursive-perl libfile-tail-perl \
    libio-socket-inet6-perl libio-socket-ssl-perl libio-tee-perl libhtml-parser-perl \
    libjson-webtoken-perl libmail-imapclient-perl libparse-recdescent-perl \
    libproc-processtable-perl libmodule-scandeps-perl libreadonly-perl \
    libregexp-common-perl libsys-meminfo-perl libterm-readkey-perl \
    libtest-mockobject-perl libunicode-string-perl liburi-perl libwww-perl >/dev/null 2>&1
REMOTE
print_ok "System packages and Perl modules installed"

#############################################################
print_step 4 "Uploading"
#############################################################
$SSH_CMD "mkdir -p $INSTALL_DIR/data/logs $INSTALL_DIR/static"

# Refuse to interrupt a migration. A restart SIGTERMs the whole cgroup, so any
# imapsync in flight dies mid-folder. It is recoverable -- the next run rescans
# and the cache means it does not recopy -- but it is not something to do to
# someone silently.
RUNNING=$($SSH_CMD "curl -s -m 5 http://127.0.0.1:8000/api/health >/dev/null 2>&1 && \
    sqlite3 $INSTALL_DIR/data/imapsync.db \
    \"select count(*) from sync_jobs where status='running'\" 2>/dev/null || echo 0")
if [ "${RUNNING:-0}" != "0" ] && [ "${FORCE:-}" != "1" ]; then
    print_warn "$RUNNING sync(s) are running right now; deploying would kill them."
    echo "  Wait for them, or re-run with FORCE=1 to deploy anyway."
    exit 1
fi

# Everything below replaces files the running binary is using, and Linux
# refuses to write to a file that is currently being executed (ETXTBSY) -- so
# the second and every later deploy failed here, after imapsync had already
# been replaced. Stop first, upload to a side name, move into place.
$SSH_CMD "systemctl stop imapsync-web 2>/dev/null || true"

scp -q -P "$SSH_PORT" "$REPO_DIR/imapsync" "$SSH_USER@$SERVER_IP:/usr/bin/imapsync.new"
$SSH_CMD "chmod +x /usr/bin/imapsync.new && mv /usr/bin/imapsync.new /usr/bin/imapsync"

scp -q -P "$SSH_PORT" "$WEB_DIR/backend-go/dist/imapsync-web" \
    "$SSH_USER@$SERVER_IP:$INSTALL_DIR/imapsync-web.new"
scp -q -P "$SSH_PORT" "$WEB_DIR/backend-go/dist/createadmin" \
    "$SSH_USER@$SERVER_IP:$INSTALL_DIR/createadmin.new"
$SSH_CMD "cd $INSTALL_DIR && chmod +x imapsync-web.new createadmin.new && \
    mv imapsync-web.new imapsync-web && mv createadmin.new createadmin"

# tar over ssh rather than rsync: tar is on every Debian install, rsync is not,
# and this is a full replace anyway so there is nothing for rsync to diff.
# Extract beside the live directory and swap, so a broken pipe mid-transfer
# leaves the old frontend serving rather than an empty directory and 404s.
tar czf - -C "$WEB_DIR/frontend/dist" . \
    | $SSH_CMD "rm -rf $INSTALL_DIR/static.new && mkdir -p $INSTALL_DIR/static.new && \
        tar xzf - -C $INSTALL_DIR/static.new && \
        rm -rf $INSTALL_DIR/static.old && \
        mv $INSTALL_DIR/static $INSTALL_DIR/static.old 2>/dev/null || true; \
        mv $INSTALL_DIR/static.new $INSTALL_DIR/static && \
        rm -rf $INSTALL_DIR/static.old"
print_ok "imapsync, binaries and frontend uploaded"

#############################################################
print_step 5 "Configuring (.env, user, permissions)"
#############################################################
$SSH_CMD "bash -s" <<REMOTE
set -e
INSTALL_DIR="$INSTALL_DIR"
DOMAIN="$DOMAIN"

if [ ! -f "\$INSTALL_DIR/.env" ]; then
    # Create it unreadable, rather than creating it 0644 and fixing it after.
    umask 077
    # A Fernet key is 32 random bytes, base64url-encoded with padding.
    FERNET_KEY=\$(head -c 32 /dev/urandom | base64 | tr '+/' '-_')
    cat > "\$INSTALL_DIR/.env" <<EOF
SECRET_KEY=\$(openssl rand -hex 32)
IMAPSYNC_FERNET_KEY=\$FERNET_KEY
DATABASE_URL=sqlite:///\$INSTALL_DIR/data/imapsync.db
IMAPSYNC_PATH=/usr/bin/imapsync
DOMAIN=\$DOMAIN
LISTEN=127.0.0.1:8000
DATA_DIR=\$INSTALL_DIR/data
STATIC_DIR=\$INSTALL_DIR/static
EOF
    chmod 600 "\$INSTALL_DIR/.env"
    echo "ENV_CREATED"
else
    echo "ENV_EXISTS (keeping it: it holds the key your stored passwords are encrypted with)"
fi

useradd -r -s /bin/false imapsync 2>/dev/null || true

# Only the data directory belongs to the service. The binaries, the frontend
# and .env stay root-owned: a service that owns its own executable can be made
# to overwrite it, and Restart=always would then run whatever replaced it.
# systemd reads EnvironmentFile as root before dropping privileges, so 600
# root-only is enough for .env.
chown -R root:root "\$INSTALL_DIR"
chown -R imapsync:imapsync "\$INSTALL_DIR/data"
chmod 600 "\$INSTALL_DIR/.env"
chmod 755 "\$INSTALL_DIR" "\$INSTALL_DIR/imapsync-web" "\$INSTALL_DIR/createadmin"
REMOTE
print_ok "Configured"

#############################################################
print_step 6 "systemd + nginx"
#############################################################
sed "s|__INSTALL_DIR__|$INSTALL_DIR|g" "$WEB_DIR/scripts/imapsync-web.service" \
    | $SSH_CMD "cat > /etc/systemd/system/imapsync-web.service"
sed "s|__DOMAIN__|$DOMAIN|g; s|__INSTALL_DIR__|$INSTALL_DIR|g" "$WEB_DIR/scripts/imapsync-web.nginx" \
    | $SSH_CMD "cat > /etc/nginx/sites-available/imapsync-web"

$SSH_CMD 'bash -s' <<'REMOTE'
set -e
systemctl daemon-reload
systemctl enable imapsync-web >/dev/null 2>&1
systemctl restart imapsync-web
ln -sf /etc/nginx/sites-available/imapsync-web /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl reload nginx
REMOTE
print_ok "Service started, nginx reloaded"

#############################################################
print_step 7 "SSL (Let's Encrypt)"
#############################################################
if $SSH_CMD "certbot --nginx -d $DOMAIN --non-interactive --agree-tos -m $SSL_EMAIL --redirect" >/dev/null 2>&1; then
    print_ok "Certificate installed for $DOMAIN"
else
    print_warn "Certbot failed — does $DOMAIN point at this server yet?"
    echo "  The site is serving plain HTTP. Do not sign in or enter mailbox"
    echo "  passwords until this succeeds:"
    echo "    certbot --nginx -d $DOMAIN"
    exit 1
fi

#############################################################
print_step 8 "Health check"
#############################################################
sleep 2
HEALTH=$($SSH_CMD "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8000/api/health" || echo "N/A")
if [ "$HEALTH" = "200" ]; then
    print_ok "Backend answering on 127.0.0.1:8000"
else
    print_err "Backend not healthy (HTTP $HEALTH). Check: journalctl -u imapsync-web -n 50"
fi

echo ""
echo -e "${GREEN}=======================================${NC}"
echo -e "${GREEN}  Deploy complete${NC}"
echo -e "${GREEN}=======================================${NC}"
echo ""
echo -e "  URL: ${CYAN}https://$DOMAIN${NC}"
echo ""
echo "  Next: create the admin account (self-registration is disabled)."
echo "    ssh -p $SSH_PORT $SSH_USER@$SERVER_IP"
echo "    cd $INSTALL_DIR && sudo -u imapsync ./createadmin"
echo ""
echo "  Service commands:"
echo "    systemctl status imapsync-web"
echo "    journalctl -u imapsync-web -f"
echo "    systemctl restart imapsync-web"
echo ""
