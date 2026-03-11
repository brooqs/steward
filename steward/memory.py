"""
memory.py — Conversation memory backed by SQLite.

Stores conversation history per session/user. Each message has a role
(user/assistant/system) and content. Old messages are pruned to keep the
context window manageable.
"""

import sqlite3
import json
import os
import time
from typing import List, Dict, Optional


class Memory:
    def __init__(self, db_path: str = "data/memory.db", max_messages: int = 50):
        self.db_path = db_path
        self.max_messages = max_messages
        os.makedirs(os.path.dirname(db_path), exist_ok=True)
        self._init_db()

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        return conn

    def _init_db(self):
        """Create tables if they don't exist."""
        with self._connect() as conn:
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

    def ensure_session(self, session_id: str, user_id: str = None, channel: str = None):
        """Create session if it doesn't exist; update timestamp if it does."""
        now = int(time.time())
        with self._connect() as conn:
            existing = conn.execute(
                "SELECT session_id FROM sessions WHERE session_id = ?", (session_id,)
            ).fetchone()
            if existing:
                conn.execute(
                    "UPDATE sessions SET updated_at = ? WHERE session_id = ?",
                    (now, session_id),
                )
            else:
                conn.execute(
                    "INSERT INTO sessions (session_id, user_id, channel, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
                    (session_id, user_id, channel, now, now),
                )

    def add_message(
        self,
        session_id: str,
        role: str,
        content: str,
        metadata: Optional[Dict] = None,
    ):
        """Append a message to the session history."""
        now = int(time.time())
        meta_json = json.dumps(metadata or {})
        with self._connect() as conn:
            conn.execute(
                "INSERT INTO messages (session_id, role, content, metadata, created_at) VALUES (?, ?, ?, ?, ?)",
                (session_id, role, content, meta_json, now),
            )
        # Prune old messages to stay within max_messages limit
        self._prune(session_id)

    def get_history(self, session_id: str, limit: Optional[int] = None) -> List[Dict]:
        """Return message history for the session (oldest first)."""
        n = limit or self.max_messages
        with self._connect() as conn:
            rows = conn.execute(
                """
                SELECT role, content FROM (
                    SELECT role, content, created_at
                    FROM messages
                    WHERE session_id = ?
                    ORDER BY created_at DESC
                    LIMIT ?
                ) ORDER BY created_at ASC
                """,
                (session_id, n),
            ).fetchall()
        return [{"role": r["role"], "content": r["content"]} for r in rows]

    def clear_session(self, session_id: str):
        """Delete all messages for a session."""
        with self._connect() as conn:
            conn.execute("DELETE FROM messages WHERE session_id = ?", (session_id,))

    def _prune(self, session_id: str):
        """Keep only the most recent max_messages messages per session."""
        with self._connect() as conn:
            count = conn.execute(
                "SELECT COUNT(*) FROM messages WHERE session_id = ?", (session_id,)
            ).fetchone()[0]
            if count > self.max_messages:
                excess = count - self.max_messages
                conn.execute(
                    """
                    DELETE FROM messages WHERE id IN (
                        SELECT id FROM messages WHERE session_id = ?
                        ORDER BY created_at ASC LIMIT ?
                    )
                    """,
                    (session_id, excess),
                )

    def list_sessions(self) -> List[Dict]:
        """Return all known sessions."""
        with self._connect() as conn:
            rows = conn.execute(
                "SELECT * FROM sessions ORDER BY updated_at DESC"
            ).fetchall()
        return [dict(r) for r in rows]
