"""
base.py — Abstract base class for Steward integrations.

Every integration must inherit from BaseIntegration and implement:
  - load_config()   — read its YAML config file
  - get_tools()     — return a list of tool specs for ToolRegistry
  - health_check()  — return True if the service is reachable
"""

import logging
import os
from abc import ABC, abstractmethod
from typing import Any, Dict, List, Optional

import yaml

logger = logging.getLogger(__name__)


class IntegrationError(Exception):
    """Raised when an integration fails to initialise or execute."""


class BaseIntegration(ABC):
    # Subclasses set this to the name of their config file (without path).
    # e.g.  config_file = "homeassistant.yml"
    config_file: str = ""

    CONFIG_DIR = os.environ.get("STEWARD_INTEGRATIONS_CONFIG", "config/integrations")

    def __init__(self):
        self.cfg: Dict[str, Any] = {}
        self.enabled: bool = False
        self._load()

    def _load(self):
        """Load config from YAML; silently disable if file is missing."""
        path = os.path.join(self.CONFIG_DIR, self.config_file)
        if not os.path.exists(path):
            logger.info(
                "%s: config not found (%s) — integration disabled.",
                self.__class__.__name__,
                path,
            )
            return
        try:
            with open(path) as f:
                self.cfg = yaml.safe_load(f) or {}
            self.load_config(self.cfg)
            self.enabled = True
            logger.info("%s: loaded config from %s", self.__class__.__name__, path)
        except Exception as exc:
            logger.error(
                "%s: failed to load config — %s",
                self.__class__.__name__,
                exc,
            )

    @abstractmethod
    def load_config(self, cfg: Dict[str, Any]):
        """Parse the loaded YAML dict and set instance attributes."""

    @abstractmethod
    def get_tools(self) -> List[Dict]:
        """
        Return a list of tool specification dicts, each with:
          {
            "name": str,
            "description": str,
            "parameters": dict (JSON Schema),
            "handler": callable,
          }
        Return [] if the integration is disabled.
        """

    @abstractmethod
    def health_check(self) -> bool:
        """Return True if the remote service is reachable."""
