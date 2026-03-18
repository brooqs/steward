# Installation

## Quick Install (Linux)

```bash
curl -sSL https://raw.githubusercontent.com/brooqs/steward/main/install.sh | bash
```

This will:
- Auto-detect your OS and architecture
- Download the latest release
- Install `steward` and `steward-satellite` to `/usr/local/bin/`
- Create `/etc/steward/` config directory
- Create `steward` system user
- Install systemd service

## Manual Install

### From Release

```bash
# Download (replace VERSION, OS, ARCH)
wget https://github.com/brooqs/steward/releases/download/v1.0.0/steward_1.0.0_linux_amd64.tar.gz
tar xzf steward_1.0.0_linux_amd64.tar.gz

# Install
sudo install -m 755 steward /usr/local/bin/
sudo install -m 755 steward-satellite /usr/local/bin/
```

### From Source

```bash
git clone https://github.com/brooqs/steward.git
cd steward
go build -o steward ./cmd/steward
go build -o steward-satellite ./cmd/satellite
```

### DEB Package (Debian/Ubuntu)

```bash
wget https://github.com/brooqs/steward/releases/download/v1.0.0/steward_1.0.0_linux_amd64.deb
sudo dpkg -i steward_1.0.0_linux_amd64.deb
```

### RPM Package (RHEL/Fedora)

```bash
wget https://github.com/brooqs/steward/releases/download/v1.0.0/steward_1.0.0_linux_amd64.rpm
sudo rpm -i steward_1.0.0_linux_amd64.rpm
```

## Docker

```bash
# Build
docker build -t steward .

# Run
docker run -d \
  --name steward \
  -v ./config:/etc/steward \
  -v steward-data:/var/lib/steward \
  -p 8080:8080 \
  steward
```

Or with docker-compose:

```bash
docker-compose up -d
```

## Post-Install Setup

```bash
# 1. Create config
sudo mkdir -p /etc/steward/integrations
sudo cp config/core.yml.example /etc/steward/core.yml

# 2. Edit config (set API key and provider)
sudo nano /etc/steward/core.yml

# 3. Create data directory
sudo mkdir -p /var/lib/steward
sudo chown steward:steward /var/lib/steward

# 4. Start
sudo systemctl enable --now steward

# 5. Verify
journalctl -u steward -f
```

## Supported Platforms

| OS | Architecture | Binary | Package |
|----|-------------|--------|---------|
| Linux | amd64 | ✅ | .deb, .rpm |
| Linux | arm64 | ✅ | .deb, .rpm |
| macOS | amd64 (Intel) | ✅ | — |
| macOS | arm64 (Apple Silicon) | ✅ | — |
| Windows | amd64 | ✅ | — |
| Windows | arm64 | ✅ | — |
