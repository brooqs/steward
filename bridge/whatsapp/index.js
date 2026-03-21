const { default: makeWASocket, useMultiFileAuthState, DisconnectReason, downloadMediaMessage } = require('@whiskeysockets/baileys');
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

async function startConnection() {
  const { state: authState, saveCreds } = await useMultiFileAuthState(DATA_DIR);

  sock = makeWASocket({
    auth: authState,
    printQRInTerminal: false,
    logger: pino({ level: 'silent' }),
    browser: ['Steward', 'Desktop', '1.0.0'],
  });

  // Save credentials on update
  sock.ev.on('creds.update', saveCreds);

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
      console.log('✅ WhatsApp connected!');
    }

    if (connection === 'close') {
      state.status = 'disconnected';
      state.qrData = null;
      const statusCode = lastDisconnect?.error?.output?.statusCode;
      const shouldReconnect = statusCode !== DisconnectReason.loggedOut;
      console.log('❌ Disconnected:', statusCode, shouldReconnect ? '— reconnecting...' : '— logged out');

      if (shouldReconnect) {
        setTimeout(startConnection, 3000);
      } else {
        state.connectedAt = null;
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

      // Extract phone number from JID (e.g. 905xxxxxxxxxx@s.whatsapp.net)
      const phone = from.replace(/@.*/, '');

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
    await sock.logout();
    state.status = 'disconnected';
    state.qrData = null;
    state.connectedAt = null;
    res.json({ status: 'logged out' });
  } catch (err) {
    res.status(500).json({ error: err.message });
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
