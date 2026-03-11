"""
jellyfin.py — Jellyfin media server integration.

Exposes tools to:
  - Search the media library
  - Get playback sessions
  - Get recently added items
"""

import logging
from typing import Any, Dict, List, Optional

import requests

from .base import BaseIntegration, IntegrationError

logger = logging.getLogger(__name__)
DEFAULT_TIMEOUT = 10


class JellyfinIntegration(BaseIntegration):
    config_file = "jellyfin.yml"

    def load_config(self, cfg: Dict[str, Any]):
        self.url = cfg.get("url", "").rstrip("/")
        self.api_key = cfg.get("api_key", "")
        if not self.url or not self.api_key:
            raise IntegrationError("Jellyfin requires 'url' and 'api_key' in config.")

    def _params(self, **kwargs) -> Dict:
        return {"api_key": self.api_key, **kwargs}

    def _get(self, path: str, **params) -> Any:
        r = requests.get(
            f"{self.url}{path}",
            params=self._params(**params),
            timeout=DEFAULT_TIMEOUT,
        )
        r.raise_for_status()
        return r.json()

    def health_check(self) -> bool:
        if not self.enabled:
            return False
        try:
            self._get("/System/Info/Public")
            return True
        except Exception as exc:
            logger.warning("Jellyfin health check failed: %s", exc)
            return False

    # ── Tool handlers ────────────────────────────────────────────────────────

    def search_library(self, query: str, media_type: Optional[str] = None) -> List[Dict]:
        """Search the Jellyfin library."""
        try:
            params: Dict[str, Any] = {"SearchTerm": query, "Limit": 10, "Recursive": True}
            if media_type:
                params["IncludeItemTypes"] = media_type
            data = self._get("/Items", **params)
            items = data.get("Items", [])
            return [
                {
                    "id": i.get("Id"),
                    "name": i.get("Name"),
                    "type": i.get("Type"),
                    "year": i.get("ProductionYear"),
                    "overview": i.get("Overview", "")[:200],
                }
                for i in items
            ]
        except Exception as exc:
            return [{"error": str(exc)}]

    def get_sessions(self) -> List[Dict]:
        """Get active playback sessions."""
        try:
            sessions = self._get("/Sessions")
            return [
                {
                    "user": s.get("UserName"),
                    "client": s.get("Client"),
                    "device": s.get("DeviceName"),
                    "now_playing": s.get("NowPlayingItem", {}).get("Name") if s.get("NowPlayingItem") else None,
                }
                for s in sessions
            ]
        except Exception as exc:
            return [{"error": str(exc)}]

    def get_recently_added(self, media_type: str = "Movie", limit: int = 5) -> List[Dict]:
        """Get recently added media."""
        try:
            data = self._get(
                "/Items/Latest",
                IncludeItemTypes=media_type,
                Limit=limit,
                Fields="Overview",
            )
            return [
                {
                    "name": i.get("Name"),
                    "type": i.get("Type"),
                    "year": i.get("ProductionYear"),
                    "overview": i.get("Overview", "")[:200],
                }
                for i in (data if isinstance(data, list) else [])
            ]
        except Exception as exc:
            return [{"error": str(exc)}]

    def get_tools(self) -> List[Dict]:
        if not self.enabled:
            return []
        return [
            {
                "name": "jellyfin_search",
                "description": "Search the Jellyfin media library for movies, TV shows, music, etc.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "Search term"},
                        "media_type": {
                            "type": "string",
                            "description": "Filter by type: Movie, Series, Episode, Audio",
                        },
                    },
                    "required": ["query"],
                },
                "handler": self.search_library,
            },
            {
                "name": "jellyfin_sessions",
                "description": "Get active Jellyfin playback sessions (who is watching what).",
                "parameters": {"type": "object", "properties": {}, "required": []},
                "handler": self.get_sessions,
            },
            {
                "name": "jellyfin_recently_added",
                "description": "Get recently added items in the Jellyfin library.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "media_type": {
                            "type": "string",
                            "description": "Media type: Movie or Series (default: Movie)",
                        },
                        "limit": {"type": "integer", "description": "Number of items (default: 5)"},
                    },
                    "required": [],
                },
                "handler": self.get_recently_added,
            },
        ]
