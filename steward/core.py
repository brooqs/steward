"""
core.py — Main agent loop.

Receives a message from any channel, runs it through Claude with the
current conversation history and available tools, and returns the
assistant's response.
"""

import logging
import os
from typing import Any, Dict, List, Optional

import anthropic
import yaml

from .memory import Memory
from .tools import ToolRegistry

logger = logging.getLogger(__name__)

CONFIG_PATH = os.environ.get("STEWARD_CONFIG", "config/core.yml")


def _load_config(path: str = CONFIG_PATH) -> Dict:
    try:
        with open(path) as f:
            return yaml.safe_load(f) or {}
    except FileNotFoundError:
        logger.warning("Config file not found: %s — using defaults", path)
        return {}


class StewardCore:
    """
    The central agent. Each call to `chat()` runs one full turn:
      1. Add the user message to memory
      2. Send history + tools to Claude
      3. Handle tool_use blocks (may loop multiple times)
      4. Return the final text response
    """

    def __init__(self, config_path: str = CONFIG_PATH):
        cfg = _load_config(config_path)

        api_key = cfg.get("anthropic_api_key") or os.environ.get("ANTHROPIC_API_KEY")
        if not api_key:
            raise ValueError(
                "Anthropic API key not set. Add it to config/core.yml or "
                "set the ANTHROPIC_API_KEY environment variable."
            )

        self.client = anthropic.Anthropic(api_key=api_key)
        self.model = cfg.get("model", "claude-3-5-sonnet-20241022")
        self.max_tokens = cfg.get("max_tokens", 4096)
        self.system_prompt = cfg.get(
            "system_prompt",
            "You are Steward, a helpful AI personal assistant. "
            "Be concise, accurate, and friendly.",
        )

        db_path = cfg.get("db_path", "data/memory.db")
        max_history = cfg.get("max_history_messages", 50)
        self.memory = Memory(db_path=db_path, max_messages=max_history)
        self.registry = ToolRegistry()

    def register_integration(self, integration):
        """Load tools from an integration into the registry."""
        self.registry.register_from_integration(integration)

    def chat(
        self,
        user_message: str,
        session_id: str = "default",
        user_id: Optional[str] = None,
        channel: Optional[str] = None,
    ) -> str:
        """
        Process one user message and return the assistant's reply.
        """
        self.memory.ensure_session(session_id, user_id=user_id, channel=channel)
        self.memory.add_message(session_id, "user", user_message)

        history = self.memory.get_history(session_id)
        tools = self.registry.get_schemas()

        response_text = self._run_turn(history, tools, session_id)
        return response_text

    def _run_turn(
        self,
        messages: List[Dict],
        tools: List[Dict],
        session_id: str,
        max_iterations: int = 5,
    ) -> str:
        """
        Run one or more Claude API calls, handling tool_use blocks until
        we get a final text response.
        """
        current_messages = list(messages)

        for iteration in range(max_iterations):
            kwargs: Dict[str, Any] = {
                "model": self.model,
                "max_tokens": self.max_tokens,
                "system": self.system_prompt,
                "messages": current_messages,
            }
            if tools:
                kwargs["tools"] = tools

            response = self.client.messages.create(**kwargs)

            # Check stop reason
            if response.stop_reason == "end_turn":
                # Extract text from response
                text = self._extract_text(response.content)
                self.memory.add_message(session_id, "assistant", text)
                return text

            if response.stop_reason == "tool_use":
                # Process all tool_use blocks
                assistant_content = response.content
                current_messages.append({"role": "assistant", "content": assistant_content})

                tool_results = []
                for block in assistant_content:
                    if block.type == "tool_use":
                        logger.info("Calling tool: %s with %s", block.name, block.input)
                        result = self.registry.dispatch(block.name, block.input)
                        tool_results.append({
                            "type": "tool_result",
                            "tool_use_id": block.id,
                            "content": self._serialize_result(result),
                        })

                current_messages.append({"role": "user", "content": tool_results})
                continue  # Next iteration

            # Unexpected stop reason — return whatever text we have
            text = self._extract_text(response.content)
            self.memory.add_message(session_id, "assistant", text)
            return text

        # Hit max_iterations — return a fallback
        fallback = "I reached the tool-call limit. Please try a simpler request."
        self.memory.add_message(session_id, "assistant", fallback)
        return fallback

    @staticmethod
    def _extract_text(content_blocks) -> str:
        """Extract all text blocks into a single string."""
        parts = []
        for block in content_blocks:
            if hasattr(block, "type") and block.type == "text":
                parts.append(block.text)
        return "\n".join(parts).strip()

    @staticmethod
    def _serialize_result(result: Any) -> str:
        """Convert tool result to a string for the API."""
        if isinstance(result, str):
            return result
        try:
            import json
            return json.dumps(result, ensure_ascii=False)
        except Exception:
            return str(result)


# Backward-compatible alias
StewardAgent = StewardCore
