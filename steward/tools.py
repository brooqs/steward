"""
tools.py — Tool registry for the Claude API tool-use feature.

Each tool is a Python callable registered with a name, description, and
JSON-schema for its parameters. The registry exposes the schema list to
pass into Claude and dispatches tool_use responses back to the right
function.
"""

import json
import logging
from typing import Any, Callable, Dict, List, Optional

logger = logging.getLogger(__name__)


class Tool:
    def __init__(
        self,
        name: str,
        description: str,
        parameters: Dict,
        handler: Callable,
    ):
        self.name = name
        self.description = description
        self.parameters = parameters
        self.handler = handler

    def to_schema(self) -> Dict:
        """Return the tool schema expected by the Anthropic API."""
        return {
            "name": self.name,
            "description": self.description,
            "input_schema": self.parameters,
        }

    def call(self, **kwargs) -> Any:
        try:
            return self.handler(**kwargs)
        except Exception as exc:
            logger.error("Tool '%s' failed: %s", self.name, exc)
            return {"error": str(exc)}


class ToolRegistry:
    def __init__(self):
        self._tools: Dict[str, Tool] = {}

    def register(
        self,
        name: str,
        description: str,
        parameters: Dict,
        handler: Callable,
    ):
        """Register a new tool."""
        self._tools[name] = Tool(name, description, parameters, handler)
        logger.debug("Registered tool: %s", name)

    def register_from_integration(self, integration):
        """
        Register all tools provided by an integration object.
        Integration must implement `get_tools() -> List[dict]` where each
        dict has keys: name, description, parameters, handler.
        """
        for spec in integration.get_tools():
            self.register(**spec)

    def get_schemas(self) -> List[Dict]:
        """Return all tool schemas for the Anthropic API."""
        return [t.to_schema() for t in self._tools.values()]

    def dispatch(self, tool_name: str, tool_input: Dict) -> Any:
        """Call the named tool with the given input."""
        if tool_name not in self._tools:
            return {"error": f"Unknown tool: {tool_name}"}
        return self._tools[tool_name].call(**tool_input)

    def list_tools(self) -> List[str]:
        return list(self._tools.keys())
