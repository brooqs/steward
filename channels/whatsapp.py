"""
whatsapp.py — WhatsApp channel adapter (stub).

WhatsApp automation requires a bridge service. Two common options:

  Option A: whatsapp-web.js (Node.js) + HTTP webhook
    - Run a small Node.js server that connects WhatsApp Web via Puppeteer
    - Configure it to POST incoming messages to Steward's webhook endpoint
    - Steward calls the Node server's REST API to send replies

  Option B: Official WhatsApp Business API (cloud or on-premise)
    - Meta Business account required
    - Webhook-based, production-grade

This module implements Option A: an aiohttp webhook server that accepts
POST /message from the whatsapp-web.js bridge and calls back to it.

See docs/whatsapp-bridge.md for the companion Node.js bridge setup.
"""

import asyncio
import json
import logging
import os
from typing import Optional

import aiohttp
from aiohttp import web

from steward.core import StewardCore

logger = logging.getLogger(__name__)

LISTEN_HOST = os.environ.get("WA_LISTEN_HOST", "0.0.0.0")
LISTEN_PORT = int(os.environ.get("WA_LISTEN_PORT", "8765"))
BRIDGE_URL = os.environ.get("WA_BRIDGE_URL", "http://localhost:3000")
SECRET = os.environ.get("WA_WEBHOOK_SECRET", "")


class WhatsAppChannel:
    def __init__(self, steward: StewardCore):
        self.steward = steward

    async def _send_reply(self, to: str, message: str):
        """Send a reply via the whatsapp-web.js bridge REST API."""
        async with aiohttp.ClientSession() as session:
            payload = {"to": to, "message": message}
            try:
                async with session.post(
                    f"{BRIDGE_URL}/send",
                    json=payload,
                    timeout=aiohttp.ClientTimeout(total=15),
                ) as resp:
                    if resp.status != 200:
                        logger.error("Bridge send failed: HTTP %s", resp.status)
            except Exception as exc:
                logger.error("Bridge send error: %s", exc)

    async def _handle_webhook(self, request: web.Request) -> web.Response:
        """Receive an incoming message from the WhatsApp bridge."""
        # Optional secret validation
        if SECRET:
            incoming_secret = request.headers.get("X-Webhook-Secret", "")
            if incoming_secret != SECRET:
                return web.Response(status=403, text="Forbidden")

        try:
            body = await request.json()
        except Exception:
            return web.Response(status=400, text="Invalid JSON")

        sender = body.get("from", "")
        text = body.get("message", "").strip()

        if not sender or not text:
            return web.Response(status=200, text="ignored")

        logger.info("WhatsApp from %s: %s", sender, text[:80])

        # Process in background so we return 200 immediately
        asyncio.create_task(self._process_and_reply(sender, text))
        return web.Response(status=200, text="ok")

    async def _process_and_reply(self, sender: str, text: str):
        try:
            # Run sync steward.chat in executor to avoid blocking the event loop
            loop = asyncio.get_event_loop()
            response = await loop.run_in_executor(
                None,
                lambda: self.steward.chat(
                    user_message=text,
                    session_id=f"whatsapp:{sender}",
                    user_id=sender,
                    channel="whatsapp",
                ),
            )
        except Exception as exc:
            logger.error("Core error: %s", exc)
            response = "Sorry, something went wrong. Please try again."

        await self._send_reply(sender, response)

    def run(self):
        """Start the aiohttp webhook server (blocking)."""
        app = web.Application()
        app.router.add_post("/message", self._handle_webhook)
        app.router.add_get("/health", lambda _: web.Response(text="ok"))

        logger.info("WhatsApp webhook listening on %s:%s", LISTEN_HOST, LISTEN_PORT)
        web.run_app(app, host=LISTEN_HOST, port=LISTEN_PORT)
