"""
telegram.py — Telegram channel adapter.

Uses the python-telegram-bot library (v20+, asyncio-based).

Each incoming message is forwarded to StewardCore.chat() and the
response is sent back to the same chat. The session_id is scoped to
the Telegram chat_id so each user/group has its own conversation history.
"""

import asyncio
import logging
import os
from typing import Optional

import yaml
from telegram import Update
from telegram.ext import (
    Application,
    CommandHandler,
    ContextTypes,
    MessageHandler,
    filters,
)

from steward.core import StewardCore

logger = logging.getLogger(__name__)

CONFIG_PATH = os.environ.get("STEWARD_CONFIG", "config/core.yml")
INTEGRATIONS_CONFIG_DIR = os.environ.get("STEWARD_INTEGRATIONS_CONFIG", "config/integrations")


def _load_telegram_token() -> str:
    """Read the Telegram bot token from core.yml or environment."""
    try:
        with open(CONFIG_PATH) as f:
            cfg = yaml.safe_load(f) or {}
        token = cfg.get("telegram_token") or os.environ.get("TELEGRAM_TOKEN", "")
    except FileNotFoundError:
        token = os.environ.get("TELEGRAM_TOKEN", "")
    if not token:
        raise ValueError(
            "Telegram bot token not set. Add 'telegram_token' to config/core.yml "
            "or set the TELEGRAM_TOKEN environment variable."
        )
    return token


class TelegramChannel:
    def __init__(self, steward: StewardCore):
        self.steward = steward

    async def _handle_message(self, update: Update, context: ContextTypes.DEFAULT_TYPE):
        """Handle a regular text message."""
        if not update.message or not update.message.text:
            return

        chat_id = str(update.effective_chat.id)
        user = update.effective_user
        user_id = str(user.id) if user else None
        text = update.message.text.strip()

        logger.info("Telegram [%s] %s: %s", chat_id, user_id, text[:80])

        # Send typing action while processing
        await context.bot.send_chat_action(chat_id=chat_id, action="typing")

        try:
            response = self.steward.chat(
                user_message=text,
                session_id=f"telegram:{chat_id}",
                user_id=user_id,
                channel="telegram",
            )
        except Exception as exc:
            logger.error("Core error: %s", exc)
            response = "Sorry, something went wrong. Please try again."

        await update.message.reply_text(response)

    async def _handle_start(self, update: Update, context: ContextTypes.DEFAULT_TYPE):
        """Handle /start command."""
        welcome = (
            "Hello! I'm Steward, your AI personal assistant. "
            "Send me a message and I'll do my best to help."
        )
        await update.message.reply_text(welcome)

    async def _handle_clear(self, update: Update, context: ContextTypes.DEFAULT_TYPE):
        """Handle /clear command — wipe conversation history."""
        chat_id = str(update.effective_chat.id)
        self.steward.memory.clear_session(f"telegram:{chat_id}")
        await update.message.reply_text("Conversation history cleared.")

    async def _handle_status(self, update: Update, context: ContextTypes.DEFAULT_TYPE):
        """Handle /status command — show available tools."""
        tools = self.steward.registry.list_tools()
        if tools:
            tool_list = "\n".join(f"  • {t}" for t in tools)
            msg = f"Active tools:\n{tool_list}"
        else:
            msg = "No tools loaded."
        await update.message.reply_text(msg)

    def run(self):
        """Start the Telegram bot (blocking)."""
        token = _load_telegram_token()
        app = Application.builder().token(token).build()

        app.add_handler(CommandHandler("start", self._handle_start))
        app.add_handler(CommandHandler("clear", self._handle_clear))
        app.add_handler(CommandHandler("status", self._handle_status))
        app.add_handler(
            MessageHandler(filters.TEXT & ~filters.COMMAND, self._handle_message)
        )

        logger.info("Telegram bot starting...")
        app.run_polling(drop_pending_updates=True)


def start(steward: Optional[StewardCore] = None):
    """Entry point: create core if not provided, then run the bot."""
    if steward is None:
        steward = StewardCore()
        # Load integrations
        from steward.integrations.homeassistant import HomeAssistantIntegration
        from steward.integrations.jellyfin import JellyfinIntegration
        from steward.integrations.qbittorrent import QBittorrentIntegration
        for integration_cls in [
            HomeAssistantIntegration,
            JellyfinIntegration,
            QBittorrentIntegration,
        ]:
            try:
                integration = integration_cls()
                if integration.enabled:
                    steward.register_integration(integration)
            except Exception as exc:
                logger.warning("Could not load integration %s: %s", integration_cls.__name__, exc)

    channel = TelegramChannel(steward)
    channel.run()


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    start()
