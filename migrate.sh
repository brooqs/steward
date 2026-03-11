#!/usr/bin/env bash
# migrate.sh — Migrate an OpenClaw workspace to Steward
#
# Usage:
#   ./migrate.sh [/path/to/openclaw/workspace] [--dry-run]
#
# Defaults to ~/.openclaw/workspace if no path given.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Activate virtualenv if it exists
if [[ -f "$SCRIPT_DIR/venv/bin/activate" ]]; then
    # shellcheck disable=SC1091
    source "$SCRIPT_DIR/venv/bin/activate"
fi

# Pass all arguments directly to the Python module
python3 -m steward.migrate "$@"
