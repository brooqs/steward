"""
homeassistant.py — Home Assistant integration.

Exposes tools to:
  - Get entity states
  - Call services (turn on/off lights, switches, etc.)
  - List entities by domain
"""

import logging
from typing import Any, Dict, List, Optional

import requests

from .base import BaseIntegration, IntegrationError

logger = logging.getLogger(__name__)

DEFAULT_TIMEOUT = 10  # seconds


class HomeAssistantIntegration(BaseIntegration):
    config_file = "homeassistant.yml"

    def load_config(self, cfg: Dict[str, Any]):
        self.url = cfg.get("url", "").rstrip("/")
        self.token = cfg.get("token", "")
        if not self.url or not self.token:
            raise IntegrationError("Home Assistant requires 'url' and 'token' in config.")

    def _headers(self) -> Dict[str, str]:
        return {
            "Authorization": f"Bearer {self.token}",
            "Content-Type": "application/json",
        }

    def _get(self, path: str) -> Any:
        r = requests.get(
            f"{self.url}/api{path}",
            headers=self._headers(),
            timeout=DEFAULT_TIMEOUT,
        )
        r.raise_for_status()
        return r.json()

    def _post(self, path: str, data: Dict) -> Any:
        r = requests.post(
            f"{self.url}/api{path}",
            headers=self._headers(),
            json=data,
            timeout=DEFAULT_TIMEOUT,
        )
        r.raise_for_status()
        return r.json()

    def health_check(self) -> bool:
        if not self.enabled:
            return False
        try:
            self._get("/")
            return True
        except Exception as exc:
            logger.warning("Home Assistant health check failed: %s", exc)
            return False

    # ── Tool handlers ────────────────────────────────────────────────────────

    def get_entity_state(self, entity_id: str) -> Dict:
        """Return the current state of a Home Assistant entity."""
        try:
            state = self._get(f"/states/{entity_id}")
            return {
                "entity_id": state["entity_id"],
                "state": state["state"],
                "attributes": state.get("attributes", {}),
                "last_updated": state.get("last_updated"),
            }
        except Exception as exc:
            return {"error": str(exc)}

    def call_service(
        self,
        domain: str,
        service: str,
        entity_id: Optional[str] = None,
        extra: Optional[Dict] = None,
    ) -> Dict:
        """Call a Home Assistant service."""
        data: Dict[str, Any] = extra or {}
        if entity_id:
            data["entity_id"] = entity_id
        try:
            result = self._post(f"/services/{domain}/{service}", data)
            return {"success": True, "result": result}
        except Exception as exc:
            return {"error": str(exc)}

    def list_entities(self, domain: Optional[str] = None) -> List[Dict]:
        """List entities, optionally filtered by domain."""
        try:
            states = self._get("/states")
            if domain:
                states = [s for s in states if s["entity_id"].startswith(f"{domain}.")]
            return [
                {
                    "entity_id": s["entity_id"],
                    "state": s["state"],
                    "friendly_name": s.get("attributes", {}).get("friendly_name", ""),
                }
                for s in states[:50]  # cap to avoid huge responses
            ]
        except Exception as exc:
            return [{"error": str(exc)}]

    # ── Tool registry ────────────────────────────────────────────────────────

    def get_tools(self) -> List[Dict]:
        if not self.enabled:
            return []
        return [
            {
                "name": "ha_get_entity_state",
                "description": (
                    "Get the current state of a Home Assistant entity. "
                    "Use this to check if a light is on, get a sensor reading, etc."
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "entity_id": {
                            "type": "string",
                            "description": "The entity ID, e.g. 'light.living_room'",
                        }
                    },
                    "required": ["entity_id"],
                },
                "handler": self.get_entity_state,
            },
            {
                "name": "ha_call_service",
                "description": (
                    "Call a Home Assistant service. Examples: turn on a light "
                    "(domain=light, service=turn_on), lock a door, run a script."
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "domain": {
                            "type": "string",
                            "description": "Service domain, e.g. 'light', 'switch', 'climate'",
                        },
                        "service": {
                            "type": "string",
                            "description": "Service name, e.g. 'turn_on', 'turn_off', 'toggle'",
                        },
                        "entity_id": {
                            "type": "string",
                            "description": "Target entity ID (optional for some services)",
                        },
                        "extra": {
                            "type": "object",
                            "description": "Additional service data (e.g. brightness, temperature)",
                        },
                    },
                    "required": ["domain", "service"],
                },
                "handler": self.call_service,
            },
            {
                "name": "ha_list_entities",
                "description": (
                    "List Home Assistant entities, optionally filtered by domain "
                    "(light, switch, sensor, climate, etc.)."
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "domain": {
                            "type": "string",
                            "description": "Domain to filter by, e.g. 'light'. Leave empty for all.",
                        }
                    },
                    "required": [],
                },
                "handler": self.list_entities,
            },
        ]
