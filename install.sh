#!/usr/bin/env bash
# install.sh — One-command Steward installer
# Usage: curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash
# Or: ./install.sh

set -euo pipefail

BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

info()    { echo -e "${GREEN}[steward]${RESET} $*"; }
warn()    { echo -e "${YELLOW}[steward]${RESET} $*"; }
error()   { echo -e "${RED}[steward]${RESET} $*" >&2; }
heading() { echo -e "\n${BOLD}$*${RESET}"; }

heading "=== Steward Installer ==="

# ── Detect environment ────────────────────────────────────────────────────────
INSTALL_DIR="${STEWARD_DIR:-$HOME/steward}"
USE_DOCKER="${USE_DOCKER:-}"

command_exists() { command -v "$1" &>/dev/null; }

# ── Clone or update repo ──────────────────────────────────────────────────────
if [ -d "$INSTALL_DIR/.git" ]; then
    info "Updating existing installation at $INSTALL_DIR"
    git -C "$INSTALL_DIR" pull --ff-only
else
    info "Cloning steward into $INSTALL_DIR"
    git clone https://github.com/brooqs/steward.git "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"

# ── Docker install ────────────────────────────────────────────────────────────
if command_exists docker && [ "${USE_DOCKER}" != "0" ]; then
    heading "Docker detected — using Docker Compose"

    if [ ! -f "config/core.yml" ]; then
        warn "No config/core.yml found — creating from defaults."
        warn "Edit $INSTALL_DIR/config/core.yml before starting."
    fi

    docker compose build
    info "Build complete."
    info ""
    info "Next steps:"
    info "  1. Edit config/core.yml — add your ANTHROPIC_API_KEY and TELEGRAM_TOKEN"
    info "  2. Copy & fill any integration configs in config/integrations/"
    info "  3. Run: cd $INSTALL_DIR && docker compose up -d"
    exit 0
fi

# ── Native Python install ─────────────────────────────────────────────────────
heading "Native Python install"

if ! command_exists python3; then
    error "python3 not found. Please install Python 3.10+ and re-run."
    exit 1
fi

PYTHON_VERSION=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
info "Using Python $PYTHON_VERSION"

# Create virtualenv
if [ ! -d ".venv" ]; then
    info "Creating virtual environment..."
    python3 -m venv .venv
fi

source .venv/bin/activate
info "Installing dependencies..."
pip install --quiet --upgrade pip
pip install --quiet -r requirements.txt

# Create data dir
mkdir -p data

# Config setup
if [ ! -f "config/core.yml" ]; then
    warn "config/core.yml not found — the defaults are in place."
    warn "Edit $INSTALL_DIR/config/core.yml and add your API keys."
fi

for example in config/integrations/*.yml.example; do
    target="${example%.example}"
    if [ ! -f "$target" ]; then
        info "Created $target (from example) — edit it to enable the integration."
        cp "$example" "$target"
    fi
done

# systemd unit (optional)
if command_exists systemctl && [ "$(id -u)" -eq 0 ]; then
    heading "Installing systemd service"
    cat > /etc/systemd/system/steward.service <<UNIT
[Unit]
Description=Steward AI Assistant
After=network.target

[Service]
Type=simple
User=$(logname 2>/dev/null || echo "$USER")
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/.venv/bin/python -m channels.telegram
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
UNIT
    systemctl daemon-reload
    info "systemd unit installed. Enable with: systemctl enable --now steward"
fi

heading "Installation complete!"
info ""
info "Next steps:"
info "  1. Edit config/core.yml  — add ANTHROPIC_API_KEY and TELEGRAM_TOKEN"
info "  2. Edit config/integrations/*.yml  — enable the services you use"
info "  3. Run:  source .venv/bin/activate && python -m channels.telegram"
