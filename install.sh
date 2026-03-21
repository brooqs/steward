#!/usr/bin/env bash
# Steward — Quick Install Script
# Usage: curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash
set -euo pipefail

VERSION="${1:-latest}"
INSTALL_DIR="/usr/local/bin"

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

# Set OS-specific paths
if [ "$OS" = "darwin" ]; then
  CONFIG_DIR="$HOME/.config/steward"
  DATA_DIR="$HOME/.local/share/steward"
else
  CONFIG_DIR="/etc/steward"
  DATA_DIR="/var/lib/steward"
fi

# Get latest version if not specified
if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -sSL https://api.github.com/repos/brooqs/steward/releases/latest | grep '"tag_name"' | cut -d'"' -f4)
fi
VERSION_NUM="${VERSION#v}"

echo "  Platform: ${OS}/${ARCH}"
echo "  Version:  ${VERSION}"

# Download and extract
URL="https://github.com/brooqs/steward/releases/download/${VERSION}/steward_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
echo "  Downloading: ${URL}"
TMP=$(mktemp -d)
curl -sSL "$URL" -o "$TMP/steward.tar.gz"
tar xzf "$TMP/steward.tar.gz" -C "$TMP"

# Install binaries
if [ "$OS" = "darwin" ]; then
  # macOS: /usr/local/bin may not exist on fresh installs
  sudo mkdir -p "$INSTALL_DIR"
  sudo cp -f "$TMP/steward" "$INSTALL_DIR/steward"
  sudo chmod 755 "$INSTALL_DIR/steward"
  if [ -f "$TMP/steward-satellite" ]; then
    sudo cp -f "$TMP/steward-satellite" "$INSTALL_DIR/steward-satellite"
    sudo chmod 755 "$INSTALL_DIR/steward-satellite"
  fi
else
  sudo install -m 755 "$TMP/steward" "$INSTALL_DIR/steward"
  sudo install -m 755 "$TMP/steward-satellite" "$INSTALL_DIR/steward-satellite" 2>/dev/null || true
fi
echo "  ✅ Binaries installed to $INSTALL_DIR"

# Setup config directory
if [ ! -d "$CONFIG_DIR" ]; then
  mkdir -p "$CONFIG_DIR/integrations"
  cp "$TMP/config/core.yml.example" "$CONFIG_DIR/core.yml.example" 2>/dev/null || true
  cp "$TMP/config/integrations/"*.yml.example "$CONFIG_DIR/integrations/" 2>/dev/null || true
  echo "  ✅ Config directory created at $CONFIG_DIR"
else
  echo "  ℹ️  Config directory already exists at $CONFIG_DIR"
fi

# Setup data directory
mkdir -p "$DATA_DIR"
echo "  ✅ Data directory created at $DATA_DIR"

# OS-specific service setup
if [ "$OS" = "darwin" ]; then
  # macOS: create launchd plist
  PLIST_DIR="$HOME/Library/LaunchAgents"
  PLIST_FILE="$PLIST_DIR/com.brooqs.steward.plist"
  mkdir -p "$PLIST_DIR"

  cat > "$PLIST_FILE" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.brooqs.steward</string>
  <key>ProgramArguments</key>
  <array>
    <string>${INSTALL_DIR}/steward</string>
    <string>-config</string>
    <string>${CONFIG_DIR}/core.yml</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>${DATA_DIR}/steward.log</string>
  <key>StandardErrorPath</key>
  <string>${DATA_DIR}/steward.err</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>${HOME}</string>
  </dict>
</dict>
</plist>
PLIST

  echo "  ✅ LaunchAgent installed at $PLIST_FILE"
else
  # Linux: create system user and systemd service
  if ! id -u steward &>/dev/null; then
    sudo useradd -r -s /bin/false -d "$DATA_DIR" steward
    sudo chown -R steward:steward "$DATA_DIR"
    echo "  ✅ System user 'steward' created"
  fi

  if [ -d /etc/systemd/system ]; then
    sudo cp "$TMP/init/steward.service" /etc/systemd/system/ 2>/dev/null || \
    curl -sSL "https://raw.githubusercontent.com/brooqs/steward/main/init/steward.service" | sudo tee /etc/systemd/system/steward.service >/dev/null
    sudo systemctl daemon-reload
    echo "  ✅ Systemd service installed"
  fi
fi

rm -rf "$TMP"

echo ""
echo "🎉 Steward installed! Next steps:"
echo ""

if [ "$OS" = "darwin" ]; then
  echo "  1. Configure: cp $CONFIG_DIR/core.yml.example $CONFIG_DIR/core.yml"
  echo "                nano $CONFIG_DIR/core.yml"
  echo ""
  echo "  2. Start:     launchctl load $PLIST_FILE"
  echo "                — or run directly: steward -config $CONFIG_DIR/core.yml"
  echo ""
  echo "  3. Logs:      tail -f $DATA_DIR/steward.log"
  echo ""
  echo "  4. Stop:      launchctl unload $PLIST_FILE"
else
  echo "  1. Configure: sudo cp $CONFIG_DIR/core.yml.example $CONFIG_DIR/core.yml"
  echo "                sudo nano $CONFIG_DIR/core.yml"
  echo ""
  echo "  2. Start:     sudo systemctl enable --now steward"
  echo ""
  echo "  3. Logs:      journalctl -u steward -f"
fi
