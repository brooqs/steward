"""
migrate.py — OpenClaw → Steward migration tool.

Imports memory files, detects integration settings, and generates
a starter config/core.yml from an existing OpenClaw workspace.

Usage:
    python3 -m steward.migrate /path/to/openclaw/workspace [--dry-run]
"""

import argparse
import os
import re
import sqlite3
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import Optional

import yaml


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _ts_now() -> int:
    return int(time.time())


def _connect(db_path: str) -> sqlite3.Connection:
    os.makedirs(os.path.dirname(db_path) or ".", exist_ok=True)
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    return conn


def _ensure_schema(conn: sqlite3.Connection):
    conn.execute("""
        CREATE TABLE IF NOT EXISTS messages (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            session_id  TEXT    NOT NULL,
            role        TEXT    NOT NULL,
            content     TEXT    NOT NULL,
            metadata    TEXT    DEFAULT '{}',
            created_at  INTEGER NOT NULL
        )
    """)
    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_session_time
        ON messages(session_id, created_at)
    """)
    conn.execute("""
        CREATE TABLE IF NOT EXISTS sessions (
            session_id  TEXT    PRIMARY KEY,
            user_id     TEXT,
            channel     TEXT,
            created_at  INTEGER NOT NULL,
            updated_at  INTEGER NOT NULL
        )
    """)
    conn.commit()


def _session_exists(conn: sqlite3.Connection, session_id: str) -> bool:
    row = conn.execute(
        "SELECT 1 FROM sessions WHERE session_id = ?", (session_id,)
    ).fetchone()
    return row is not None


def _insert_session(conn: sqlite3.Connection, session_id: str, channel: str = "openclaw"):
    now = _ts_now()
    conn.execute(
        "INSERT OR IGNORE INTO sessions (session_id, user_id, channel, created_at, updated_at) "
        "VALUES (?, ?, ?, ?, ?)",
        (session_id, "openclaw_import", channel, now, now),
    )
    conn.commit()


def _insert_message(conn: sqlite3.Connection, session_id: str, role: str, content: str, ts: int):
    conn.execute(
        "INSERT INTO messages (session_id, role, content, metadata, created_at) VALUES (?, ?, ?, ?, ?)",
        (session_id, role, content, '{"source": "openclaw_migration"}', ts),
    )


# ---------------------------------------------------------------------------
# 1. Memory migration
# ---------------------------------------------------------------------------

def _parse_markdown_to_messages(text: str, base_ts: int):
    """
    Split a markdown file into rough message chunks.
    Each top-level heading (##) or bold line becomes a 'user' or 'assistant'
    block. Plain paragraphs are stored as 'assistant' context.
    Returns list of (role, content, ts_offset).
    """
    messages = []
    current_lines = []
    offset = 0

    def flush(lines, role):
        nonlocal offset
        chunk = "\n".join(lines).strip()
        if chunk:
            messages.append((role, chunk, base_ts + offset))
            offset += 1

    for line in text.splitlines():
        # New heading → flush previous block
        if line.startswith("## ") or line.startswith("### "):
            flush(current_lines, "assistant")
            current_lines = [line]
        else:
            current_lines.append(line)

    flush(current_lines, "assistant")
    return messages


def parse_memory(openclaw_path: Path, db_path: str, dry_run: bool = False):
    """
    Import MEMORY.md and memory/*.md into Steward's SQLite memory.
    Returns (files_imported, messages_imported).
    """
    files_imported = 0
    messages_imported = 0

    if dry_run:
        conn = None
    else:
        conn = _connect(db_path)
        _ensure_schema(conn)

    # --- MEMORY.md → long_term session ---
    memory_md = openclaw_path / "MEMORY.md"
    if memory_md.exists():
        text = memory_md.read_text(encoding="utf-8", errors="replace")
        session_id = "long_term"
        msgs = _parse_markdown_to_messages(text, _ts_now() - 86400 * 365)
        if not dry_run and msgs:
            if _session_exists(conn, session_id):
                print(f"  ⚠  Session '{session_id}' already exists — skipping MEMORY.md")
            else:
                _insert_session(conn, session_id, channel="long_term")
                for role, content, ts in msgs:
                    _insert_message(conn, session_id, role, content, ts)
                conn.commit()
        files_imported += 1
        messages_imported += len(msgs)

    # --- memory/YYYY-MM-DD.md → dated sessions ---
    memory_dir = openclaw_path / "memory"
    if memory_dir.exists():
        date_pattern = re.compile(r"^\d{4}-\d{2}-\d{2}\.md$")
        for md_file in sorted(memory_dir.glob("*.md")):
            if not date_pattern.match(md_file.name):
                continue
            date_str = md_file.stem  # e.g. "2025-03-10"
            try:
                dt = datetime.strptime(date_str, "%Y-%m-%d")
                base_ts = int(dt.timestamp())
            except ValueError:
                base_ts = _ts_now()

            session_id = f"daily_{date_str}"
            text = md_file.read_text(encoding="utf-8", errors="replace")
            msgs = _parse_markdown_to_messages(text, base_ts)

            if not dry_run and msgs:
                if _session_exists(conn, session_id):
                    print(f"  ⚠  Session '{session_id}' already exists — skipping")
                else:
                    _insert_session(conn, session_id, channel="daily")
                    for role, content, ts in msgs:
                        _insert_message(conn, session_id, role, content, ts)
                    conn.commit()
            files_imported += 1
            messages_imported += len(msgs)

    if conn:
        conn.close()

    return files_imported, messages_imported


# ---------------------------------------------------------------------------
# 2. Integration detection from TOOLS.md
# ---------------------------------------------------------------------------

def _extract_between(text: str, start_marker: str, end_markers: list) -> Optional[str]:
    """Extract a block of text between start_marker and the next end_marker."""
    idx = text.find(start_marker)
    if idx == -1:
        return None
    block_start = idx + len(start_marker)
    block_end = len(text)
    for em in end_markers:
        ei = text.find(em, block_start)
        if ei != -1 and ei < block_end:
            block_end = ei
    return text[block_start:block_end]


def parse_tools(tools_md_path: Path, output_dir: Path, dry_run: bool = False):
    """
    Parse TOOLS.md and extract integration configs.
    Returns dict of {integration_name: status} where status is
    'ok', 'partial', or 'missing'.
    """
    results = {}

    if not tools_md_path.exists():
        return results

    text = tools_md_path.read_text(encoding="utf-8", errors="replace")

    # -----------------------------------------------------------------------
    # Home Assistant
    # -----------------------------------------------------------------------
    ha_cfg = {}

    # Look for proxy URL
    proxy_match = re.search(r"Server:\s*(https?://\S+)", text[text.find("Home Assistant"):] if "Home Assistant" in text else text)
    # More targeted: find the HA Proxy section
    ha_proxy_section = _extract_between(text, "### Home Assistant Proxy", ["###", "---"])
    if ha_proxy_section:
        url_m = re.search(r"Server:\s*(https?://[^\s\n]+)", ha_proxy_section)
        if url_m:
            ha_cfg["url"] = url_m.group(1).strip()
        # Token note — token is managed externally, note that
        token_m = re.search(r"Token\s+[`'\"]?([/\w.]+)[`'\"]?\s+(?:içinde|is|managed)", ha_proxy_section)
        if token_m:
            ha_cfg["token_path"] = token_m.group(1).strip()

    # Fall back to direct HA section if no proxy found
    if not ha_cfg.get("url"):
        ha_direct = _extract_between(text, "### Home Assistant", ["###", "---"])
        if ha_direct:
            url_m = re.search(r"(?:Server|URL|Host):\s*(https?://[^\s\n]+)", ha_direct)
            if url_m:
                ha_cfg["url"] = url_m.group(1).strip()
            token_m = re.search(r"[Tt]oken:\s*([A-Za-z0-9._\-]{20,})", ha_direct)
            if token_m:
                ha_cfg["token"] = token_m.group(1).strip()

    ha_status = "missing"
    if ha_cfg.get("url"):
        ha_status = "ok" if (ha_cfg.get("token") or ha_cfg.get("token_path")) else "partial"
        ha_yml = {
            "homeassistant": {
                "url": ha_cfg.get("url", "http://homeassistant.local:8123"),
                "token": ha_cfg.get("token", "REPLACE_WITH_HA_TOKEN"),
            }
        }
        if ha_cfg.get("token_path"):
            ha_yml["homeassistant"]["token_path"] = ha_cfg["token_path"]
            ha_yml["homeassistant"]["note"] = "Token managed externally; see token_path"
        _write_integration_yml(output_dir / "homeassistant.yml", ha_yml, dry_run)
    results["homeassistant"] = ha_status

    # -----------------------------------------------------------------------
    # Jellyfin
    # -----------------------------------------------------------------------
    jf_cfg = {}
    jf_section = _extract_between(text, "### Jellyfin", ["###", "---"])
    if jf_section:
        server_m = re.search(r"Server:\s*([\d.]+:\d+)", jf_section)
        if server_m:
            jf_cfg["url"] = f"http://{server_m.group(1).strip()}"
        apikey_m = re.search(r"[Aa][Pp][Ii]\s*[Kk]ey:\s*([A-Za-z0-9]{20,})", jf_section)
        if apikey_m:
            jf_cfg["api_key"] = apikey_m.group(1).strip()

    jf_status = "missing"
    if jf_cfg.get("url"):
        jf_status = "ok" if jf_cfg.get("api_key") else "partial"
        jf_yml = {
            "jellyfin": {
                "url": jf_cfg.get("url", "http://jellyfin.local:8096"),
                "api_key": jf_cfg.get("api_key", "REPLACE_WITH_JELLYFIN_API_KEY"),
            }
        }
        _write_integration_yml(output_dir / "jellyfin.yml", jf_yml, dry_run)
    results["jellyfin"] = jf_status

    # -----------------------------------------------------------------------
    # qBittorrent
    # -----------------------------------------------------------------------
    qbt_cfg = {}
    qbt_section = _extract_between(text, "### qBittorrent", ["###", "---"])
    if qbt_section:
        server_m = re.search(r"Server:\s*([\d.]+:\d+)", qbt_section)
        if server_m:
            qbt_cfg["url"] = f"http://{server_m.group(1).strip()}"
        user_m = re.search(r"[Uu]ser(?:name)?:\s*(\S+)", qbt_section)
        if user_m:
            qbt_cfg["username"] = user_m.group(1).strip()
        pass_m = re.search(r"[Pp]ass(?:word)?:\s*(\S+)", qbt_section)
        if pass_m:
            qbt_cfg["password"] = pass_m.group(1).strip()

    qbt_status = "missing"
    if qbt_cfg.get("url"):
        qbt_status = "ok" if (qbt_cfg.get("username") and qbt_cfg.get("password")) else "partial"
        qbt_yml = {
            "qbittorrent": {
                "url": qbt_cfg.get("url", "http://localhost:8081"),
                "username": qbt_cfg.get("username", "admin"),
                "password": qbt_cfg.get("password", "REPLACE_WITH_QBITTORRENT_PASSWORD"),
            }
        }
        _write_integration_yml(output_dir / "qbittorrent.yml", qbt_yml, dry_run)
    results["qbittorrent"] = qbt_status

    return results


def _write_integration_yml(path: Path, data: dict, dry_run: bool):
    """Write an integration YAML, refusing to overwrite existing files."""
    if path.exists():
        print(f"  ⚠  {path} already exists — skipping (manual merge needed)")
        return
    if dry_run:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w") as f:
        yaml.dump(data, f, default_flow_style=False, allow_unicode=True)


# ---------------------------------------------------------------------------
# 3. Core config generation
# ---------------------------------------------------------------------------

def parse_config(openclaw_path: Path, output_path: Path, dry_run: bool = False):
    """
    Detect Claude API key and generate config/core.yml.
    Returns True if generated, False if already existed.
    """
    if output_path.exists():
        print(f"  ⚠  {output_path} already exists — skipping")
        return False

    api_key = ""

    # Try common openclaw config locations
    candidates = [
        openclaw_path / ".env",
        openclaw_path.parent / ".env",
        openclaw_path / "config.ini",
        openclaw_path / "config.yml",
        openclaw_path / "config.yaml",
        Path.home() / ".openclaw" / "config.yml",
        Path.home() / ".openclaw" / ".env",
        Path("/root/.openclaw/config.yml"),
        Path("/root/.openclaw/.env"),
    ]
    key_patterns = [
        re.compile(r"ANTHROPIC_API_KEY[=:]\s*([^\s\"']+)"),
        re.compile(r"anthropic_api_key[=:]\s*[\"']?([^\s\"']+)[\"']?"),
        re.compile(r"claude_api_key[=:]\s*[\"']?([^\s\"']+)[\"']?"),
    ]

    for candidate in candidates:
        try:
            if not candidate.exists():
                continue
            content = candidate.read_text(encoding="utf-8", errors="replace")
            for pat in key_patterns:
                m = pat.search(content)
                if m:
                    api_key = m.group(1).strip()
                    break
        except (PermissionError, OSError, Exception):
            continue
        if api_key:
            break

    # Also check environment
    if not api_key:
        api_key = os.environ.get("ANTHROPIC_API_KEY", "")

    core_cfg = {
        "anthropic_api_key": api_key or "REPLACE_WITH_YOUR_API_KEY",
        "model": "claude-3-5-sonnet-20241022",
        "max_tokens": 4096,
        "max_history_messages": 50,
        "db_path": "data/memory.db",
        "system_prompt": (
            "You are Steward, a helpful AI personal assistant. "
            "Be concise, accurate, and friendly."
        ),
    }

    if not dry_run:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        with open(output_path, "w") as f:
            yaml.dump(core_cfg, f, default_flow_style=False, allow_unicode=True)

    return True


# ---------------------------------------------------------------------------
# Main CLI
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description="Migrate an OpenClaw workspace to Steward."
    )
    parser.add_argument(
        "openclaw_path",
        nargs="?",
        default=str(Path.home() / ".openclaw" / "workspace"),
        help="Path to the OpenClaw workspace directory (default: ~/.openclaw/workspace)",
    )
    parser.add_argument(
        "--db-path",
        default="data/memory.db",
        help="Path to Steward's SQLite database (default: data/memory.db)",
    )
    parser.add_argument(
        "--config-dir",
        default="config",
        help="Path to Steward's config directory (default: config)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be done without making any changes",
    )
    args = parser.parse_args()

    openclaw = Path(args.openclaw_path).expanduser().resolve()
    config_dir = Path(args.config_dir)
    integrations_dir = config_dir / "integrations"
    core_yml = config_dir / "core.yml"

    if not openclaw.exists():
        print(f"✗ OpenClaw workspace not found: {openclaw}", file=sys.stderr)
        sys.exit(1)

    if args.dry_run:
        print("🔍 Dry-run mode — no changes will be made.\n")

    print(f"Migrating from: {openclaw}\n")

    # --- 1. Memory ---
    print("Importing memory...")
    tools_md = openclaw / "TOOLS.md"
    files, msgs = parse_memory(openclaw, args.db_path, dry_run=args.dry_run)
    if files:
        print(f"✓ Memory: {files} file(s) imported ({msgs} message chunks)")
    else:
        print("⚠ Memory: No MEMORY.md or memory/*.md files found")

    # --- 2. Integrations ---
    print("\nDetecting integrations...")
    if tools_md.exists():
        statuses = parse_tools(tools_md, integrations_dir, dry_run=args.dry_run)
    else:
        statuses = {}
        print("  ⚠  TOOLS.md not found — skipping integration detection")

    ok_integrations = [k for k, v in statuses.items() if v == "ok"]
    partial_integrations = [k for k, v in statuses.items() if v == "partial"]
    missing_integrations = [k for k, v in statuses.items() if v == "missing"]

    total = len(statuses)
    detected = len(ok_integrations) + len(partial_integrations)

    if ok_integrations or partial_integrations:
        names = ", ".join(ok_integrations + partial_integrations)
        print(f"✓ Integrations: {names} ({detected}/{total} detected)")
    for k in partial_integrations:
        print(f"⚠ Integrations: {k} (config incomplete — manual setup needed)")
    for k in missing_integrations:
        if statuses:  # only show if we actually parsed the file
            print(f"✗ Integrations: {k} (not found in TOOLS.md)")

    # --- 3. Config ---
    print("\nGenerating core config...")
    generated = parse_config(openclaw, core_yml, dry_run=args.dry_run)
    if generated:
        print(f"✓ Config: {core_yml} generated")
    else:
        print(f"⚠ Config: {core_yml} already exists — skipped")

    print("\nMigration complete.")
    if args.dry_run:
        print("(Dry-run: no files were written)")


if __name__ == "__main__":
    main()
