#!/usr/bin/env bash
# Steward — Quick Install Script
# Usage: curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash
set -euo pipefail

VERSION="${1:-latest}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/steward"
DATA_DIR="/var/lib/steward"

echo "🤖 Installing Steward..."

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version if not specified
if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -sSL https://api.github.com/repos/brooqs/steward/releases/latest | grep '"tag_name"' | cut -d'"' -f4)
fi
VERSION_NUM="${VERSION#v}"

echo "  Platform: ${OS}/${ARCH}"
echo "  Version:  ${VERSION}"

# Download
URL="https://github.com/brooqs/steward/releases/download/${VERSION}/steward_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
echo "  Downloading: ${URL}"
TMP=$(mktemp -d)
curl -sSL "$URL" -o "$TMP/steward.tar.gz"
tar xzf "$TMP/steward.tar.gz" -C "$TMP"

# Install binaries
sudo install -m 755 "$TMP/steward" "$INSTALL_DIR/steward"
sudo install -m 755 "$TMP/steward-satellite" "$INSTALL_DIR/steward-satellite" 2>/dev/null || true
echo "  ✅ Binaries installed to $INSTALL_DIR"

# Setup config directory
if [ ! -d "$CONFIG_DIR" ]; then
  sudo mkdir -p "$CONFIG_DIR/integrations"
  sudo cp "$TMP/config/core.yml.example" "$CONFIG_DIR/core.yml.example"
  sudo cp "$TMP/config/integrations/"*.yml.example "$CONFIG_DIR/integrations/" 2>/dev/null || true
  echo "  ✅ Config directory created at $CONFIG_DIR"
fi

# Setup data directory
sudo mkdir -p "$DATA_DIR"
echo "  ✅ Data directory created at $DATA_DIR"

# Create steward user if doesn't exist
if ! id -u steward &>/dev/null; then
  sudo useradd -r -s /bin/false -d "$DATA_DIR" steward
  sudo chown -R steward:steward "$DATA_DIR"
  echo "  ✅ System user 'steward' created"
fi

# Install systemd service
if [ -d /etc/systemd/system ]; then
  sudo cp "$TMP/init/steward.service" /etc/systemd/system/ 2>/dev/null || \
  curl -sSL "https://raw.githubusercontent.com/brooqs/steward/main/init/steward.service" | sudo tee /etc/systemd/system/steward.service >/dev/null
  sudo systemctl daemon-reload
  echo "  ✅ Systemd service installed"
fi

rm -rf "$TMP"

echo ""
echo "🎉 Steward installed! Next steps:"
echo ""
echo "  1. Configure: sudo cp $CONFIG_DIR/core.yml.example $CONFIG_DIR/core.yml"
echo "                sudo nano $CONFIG_DIR/core.yml"
echo ""
echo "  2. Start:     sudo systemctl enable --now steward"
echo ""
echo "  3. Logs:      journalctl -u steward -f"
