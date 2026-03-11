"""
qbittorrent.py — qBittorrent integration.

Exposes tools to:
  - List active/completed torrents
  - Add a torrent by URL or magnet
  - Pause / resume torrents
"""

import logging
from typing import Any, Dict, List, Optional

import requests

from .base import BaseIntegration, IntegrationError

logger = logging.getLogger(__name__)
DEFAULT_TIMEOUT = 10


class QBittorrentIntegration(BaseIntegration):
    config_file = "qbittorrent.yml"

    def load_config(self, cfg: Dict[str, Any]):
        self.url = cfg.get("url", "").rstrip("/")
        self.username = cfg.get("username", "admin")
        self.password = cfg.get("password", "")
        if not self.url:
            raise IntegrationError("qBittorrent requires 'url' in config.")
        self._session = requests.Session()
        self._logged_in = False

    def _login(self):
        r = self._session.post(
            f"{self.url}/api/v2/auth/login",
            data={"username": self.username, "password": self.password},
            timeout=DEFAULT_TIMEOUT,
        )
        if r.text == "Ok.":
            self._logged_in = True
        else:
            raise IntegrationError(f"qBittorrent login failed: {r.text}")

    def _ensure_login(self):
        if not self._logged_in:
            self._login()

    def _get(self, path: str, **params) -> Any:
        self._ensure_login()
        r = self._session.get(f"{self.url}/api/v2{path}", params=params, timeout=DEFAULT_TIMEOUT)
        r.raise_for_status()
        return r.json()

    def _post(self, path: str, data: Dict) -> str:
        self._ensure_login()
        r = self._session.post(f"{self.url}/api/v2{path}", data=data, timeout=DEFAULT_TIMEOUT)
        r.raise_for_status()
        return r.text

    def health_check(self) -> bool:
        if not self.enabled:
            return False
        try:
            self._ensure_login()
            return True
        except Exception as exc:
            logger.warning("qBittorrent health check failed: %s", exc)
            return False

    # ── Tool handlers ────────────────────────────────────────────────────────

    def list_torrents(self, filter: str = "all") -> List[Dict]:
        """List torrents. filter: all, active, completed, paused."""
        try:
            torrents = self._get("/torrents/info", filter=filter)
            return [
                {
                    "name": t.get("name"),
                    "state": t.get("state"),
                    "progress": round(t.get("progress", 0) * 100, 1),
                    "size_gb": round(t.get("size", 0) / 1e9, 2),
                    "eta_seconds": t.get("eta"),
                    "hash": t.get("hash"),
                }
                for t in (torrents or [])[:20]
            ]
        except Exception as exc:
            return [{"error": str(exc)}]

    def add_torrent(self, url: str, save_path: Optional[str] = None) -> Dict:
        """Add a torrent by URL or magnet link."""
        try:
            data: Dict[str, Any] = {"urls": url}
            if save_path:
                data["savepath"] = save_path
            result = self._post("/torrents/add", data)
            return {"success": result == "Ok.", "message": result}
        except Exception as exc:
            return {"error": str(exc)}

    def pause_torrent(self, torrent_hash: str) -> Dict:
        """Pause a torrent by its hash."""
        try:
            self._post("/torrents/pause", {"hashes": torrent_hash})
            return {"success": True}
        except Exception as exc:
            return {"error": str(exc)}

    def resume_torrent(self, torrent_hash: str) -> Dict:
        """Resume a paused torrent by its hash."""
        try:
            self._post("/torrents/resume", {"hashes": torrent_hash})
            return {"success": True}
        except Exception as exc:
            return {"error": str(exc)}

    def get_tools(self) -> List[Dict]:
        if not self.enabled:
            return []
        return [
            {
                "name": "qbt_list_torrents",
                "description": "List torrents in qBittorrent. Can filter by status.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "filter": {
                            "type": "string",
                            "description": "all | active | completed | paused (default: all)",
                        }
                    },
                    "required": [],
                },
                "handler": self.list_torrents,
            },
            {
                "name": "qbt_add_torrent",
                "description": "Add a torrent to qBittorrent by magnet link or .torrent URL.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "url": {"type": "string", "description": "Magnet link or torrent URL"},
                        "save_path": {"type": "string", "description": "Optional save directory"},
                    },
                    "required": ["url"],
                },
                "handler": self.add_torrent,
            },
            {
                "name": "qbt_pause_torrent",
                "description": "Pause a torrent by its hash.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "torrent_hash": {"type": "string", "description": "Torrent hash"}
                    },
                    "required": ["torrent_hash"],
                },
                "handler": self.pause_torrent,
            },
            {
                "name": "qbt_resume_torrent",
                "description": "Resume a paused torrent by its hash.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "torrent_hash": {"type": "string", "description": "Torrent hash"}
                    },
                    "required": ["torrent_hash"],
                },
                "handler": self.resume_torrent,
            },
        ]
