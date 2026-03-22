const { default: makeWASocket, useMultiFileAuthState, DisconnectReason, fetchLatestBaileysVersion, downloadMediaMessage } = require('@whiskeysockets/baileys');
const qrcode = require('qrcode-terminal');
const express = require('express');
const pino = require('pino');
const path = require('path');

const STEWARD_URL = process.env.STEWARD_URL || 'http://127.0.0.1:8765';
const WEBHOOK_SECRET = process.env.WEBHOOK_SECRET || '';
const PORT = process.env.PORT || 3000;
const DATA_DIR = process.env.DATA_DIR || path.join(process.env.HOME || '.', '.local', 'share', 'steward', 'whatsapp-session');

const app = express();
app.use(express.json());

// CORS for admin panel
app.use((req, res, next) => {
  res.header('Access-Control-Allow-Origin', '*');
  res.header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
  res.header('Access-Control-Allow-Headers', 'Content-Type');
  if (req.method === 'OPTIONS') return res.sendStatus(200);
  next();
});

let state = {
  status: 'initializing',
  qrData: null,
  connectedAt: null,
  messageCount: 0
};

let sock = null;
let reconnectAttempt = 0;

// LID → phone number mapping
const lidToPhone = new Map();

async function startConnection() {
  // Close existing socket if any
  if (sock) {
    try { sock.end(); } catch {}
    sock = null;
  }

  const { state: authState, saveCreds } = await useMultiFileAuthState(DATA_DIR);

  // Fetch latest WhatsApp version to avoid 405 errors
  let version;
  try {
    const latest = await fetchLatestBaileysVersion();
    version = latest.version;
    console.log('📋 WhatsApp version:', version.join('.'));
  } catch (err) {
    console.warn('⚠️  Could not fetch latest version, using default');
  }

  sock = makeWASocket({
    version,
    auth: authState,
    printQRInTerminal: false,
    logger: pino({ level: 'silent' }),
    browser: ['Steward', 'Desktop', '1.0.0'],
  });

  // Save credentials on update
  sock.ev.on('creds.update', saveCreds);

  // Build LID ↔ phone mapping from contacts
  sock.ev.on('contacts.upsert', (contacts) => {
    for (const c of contacts) {
      if (c.id && c.lid) {
        const phone = c.id.replace(/@.*/, '');
        const lid = c.lid.replace(/@.*/, '');
        lidToPhone.set(lid, phone);
        console.log('📇 Contact mapped: LID ' + lid + ' → ' + phone);
      }
    }
  });
  sock.ev.on('contacts.update', (contacts) => {
    for (const c of contacts) {
      if (c.id && c.lid) {
        const phone = c.id.replace(/@.*/, '');
        const lid = c.lid.replace(/@.*/, '');
        lidToPhone.set(lid, phone);
      }
    }
  });

  // Connection updates
  sock.ev.on('connection.update', (update) => {
    const { connection, lastDisconnect, qr } = update;

    if (qr) {
      state.status = 'qr';
      state.qrData = qr;
      console.log('\n📱 QR code ready — scan via Admin Panel or terminal:');
      qrcode.generate(qr, { small: true });
    }

    if (connection === 'open') {
      state.status = 'ready';
      state.qrData = null;
      state.connectedAt = new Date().toISOString();
      reconnectAttempt = 0;
      console.log('✅ WhatsApp connected!');
    }

    if (connection === 'close') {
      state.status = 'disconnected';
      state.qrData = null;
      const statusCode = lastDisconnect?.error?.output?.statusCode;
      const shouldReconnect = statusCode !== DisconnectReason.loggedOut;
      console.log('❌ Disconnected:', statusCode, shouldReconnect ? '— reconnecting...' : '— logged out');

      if (shouldReconnect) {
        reconnectAttempt++;
        const delay = Math.min(3000 * reconnectAttempt, 30000);
        console.log('   Retry in ' + (delay / 1000) + 's (attempt ' + reconnectAttempt + ')');
        setTimeout(startConnection, delay);
      } else {
        // Logged out — clear session and restart for fresh QR
        state.connectedAt = null;
        console.log('🗑️  Clearing session for fresh QR...');
        try {
          const fs = require('fs');
          fs.rmSync(DATA_DIR, { recursive: true, force: true });
          console.log('✅ Session cleared');
        } catch (e) {
          console.log('⚠️  Session clear error:', e.message);
        }
        reconnectAttempt = 0;
        // Wait longer to avoid WhatsApp rate limiting
        console.log('   Restarting in 5s...');
        setTimeout(startConnection, 5000);
      }
    }
  });

  // Incoming messages
  sock.ev.on('messages.upsert', async ({ messages, type }) => {
    if (type !== 'notify') return;

    for (const msg of messages) {
      if (!msg.message || msg.key.fromMe) continue;

      const from = msg.key.remoteJid;
      state.messageCount++;

      // Extract phone number — resolve LID to real phone if possible
      const rawId = from.replace(/@.*/, '');
      const isLid = from.endsWith('@lid');
      const phone = isLid ? (lidToPhone.get(rawId) || rawId) : rawId;
      if (isLid && !lidToPhone.has(rawId)) {
        console.log('⚠️  Unknown LID: ' + rawId + ' — add to allow list or map contact');
      }

      try {
        const headers = { 'Content-Type': 'application/json' };
        if (WEBHOOK_SECRET) headers['X-Webhook-Secret'] = WEBHOOK_SECRET;

        // Voice message
        const audioMsg = msg.message.audioMessage;
        if (audioMsg) {
          console.log('🎤 ' + from + ' (' + phone + '): [voice message]');
          try {
            const buffer = await downloadMediaMessage(msg, 'buffer', {});
            const audioBase64 = buffer.toString('base64');
            const res = await fetch(STEWARD_URL + '/message', {
              method: 'POST',
              headers,
              body: JSON.stringify({
                from,
                phone,
                message: '',
                audio_base64: audioBase64,
                audio_mimetype: audioMsg.mimetype || 'audio/ogg; codecs=opus'
              })
            });
            console.log('→ Steward (voice): ' + res.status);
          } catch (err) {
            console.error('→ Voice download error:', err.message);
          }
          continue;
        }

        // Text message
        const text = msg.message.conversation
          || msg.message.extendedTextMessage?.text
          || '';
        if (!text.trim()) continue;

        console.log('📩 ' + from + ' (' + phone + '): ' + text.substring(0, 80));

        const res = await fetch(STEWARD_URL + '/message', {
          method: 'POST',
          headers,
          body: JSON.stringify({ from, phone, message: text.trim() })
        });
        console.log('→ Steward: ' + res.status);
      } catch (err) {
        console.error('→ Steward error:', err.message);
      }
    }
  });
}

// ── API Endpoints ──────────────────────────────────────────

// Health + status
app.get('/health', (req, res) => {
  res.json({
    status: state.status,
    hasQR: state.qrData !== null,
    connectedAt: state.connectedAt,
    messageCount: state.messageCount,
    uptime: process.uptime()
  });
});

// QR code data (for admin panel)
app.get('/qr', (req, res) => {
  if (!state.qrData) {
    return res.json({ available: false, status: state.status });
  }
  res.json({ available: true, data: state.qrData });
});

// Send message (called by Steward)
app.post('/send', async (req, res) => {
  const { to, message } = req.body;
  if (!to || !message) {
    return res.status(400).json({ error: 'to and message required' });
  }

  try {
    await sock.sendMessage(to, { text: message });
    console.log('📤 → ' + to + ': ' + message.substring(0, 80));
    res.json({ status: 'sent' });
  } catch (err) {
    console.error('Send error:', err.message);
    res.status(500).json({ error: err.message });
  }
});

// Logout (disconnect WhatsApp session)
app.post('/logout', async (req, res) => {
  try {
    if (sock) await sock.logout();
    state.status = 'disconnected';
    state.qrData = null;
    state.connectedAt = null;
    res.json({ status: 'logged out — generating new QR...' });
    // Connection close handler will clear session and restart
  } catch (err) {
    // Force restart even if logout fails
    state.status = 'disconnected';
    state.qrData = null;
    state.connectedAt = null;
    const fs = require('fs');
    try { fs.rmSync(DATA_DIR, { recursive: true, force: true }); } catch {}
    reconnectAttempt = 0;
    setTimeout(startConnection, 2000);
    res.json({ status: 'session cleared — generating new QR...' });
  }
});

// Start
app.listen(PORT, () => {
  console.log('🌉 Steward WhatsApp Bridge (Baileys)');
  console.log('   API: http://0.0.0.0:' + PORT);
  console.log('   Steward: ' + STEWARD_URL);
  console.log('   Session: ' + DATA_DIR);
  startConnection();
});
