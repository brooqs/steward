const { Client, LocalAuth } = require('whatsapp-web.js');
const qrcode = require('qrcode-terminal');
const express = require('express');

const STEWARD_URL = process.env.STEWARD_URL || 'http://127.0.0.1:8765';
const WEBHOOK_SECRET = process.env.WEBHOOK_SECRET || '';
const PORT = process.env.PORT || 3000;

const app = express();
app.use(express.json());

// WhatsApp client with persistent session
const client = new Client({
  authStrategy: new LocalAuth({ dataPath: '/opt/whatsapp-bridge/session' }),
  puppeteer: {
    executablePath: '/usr/bin/chromium',
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage']
  }
});

let state = {
  status: 'initializing',  // initializing | qr | authenticated | ready | disconnected
  qrData: null,
  connectedAt: null,
  messageCount: 0
};

client.on('qr', (qr) => {
  state.status = 'qr';
  state.qrData = qr;
  console.log('\n📱 QR code ready — scan via Admin Panel or terminal:');
  qrcode.generate(qr, { small: true });
});

client.on('ready', () => {
  state.status = 'ready';
  state.qrData = null;
  state.connectedAt = new Date().toISOString();
  console.log('✅ WhatsApp client ready!');
});

client.on('authenticated', () => {
  state.status = 'authenticated';
  state.qrData = null;
  console.log('🔐 Authenticated');
});

client.on('disconnected', (reason) => {
  state.status = 'disconnected';
  state.qrData = null;
  state.connectedAt = null;
  console.log('❌ Disconnected:', reason);
  setTimeout(() => {
    state.status = 'initializing';
    client.initialize();
  }, 5000);
});

// Forward incoming messages to Steward
client.on('message', async (msg) => {
  if (msg.isStatus) return;

  const from = msg.from;
  state.messageCount++;

  // Resolve phone number from contact
  let phone = '';
  try {
    const contact = await msg.getContact();
    phone = contact.number || '';  // e.g. "905xxxxxxxxxx"
  } catch {}

  try {
    const headers = { 'Content-Type': 'application/json' };
    if (WEBHOOK_SECRET) headers['X-Webhook-Secret'] = WEBHOOK_SECRET;

    // Voice message (ptt = push-to-talk voice note, audio = audio file)
    if (msg.hasMedia && (msg.type === 'ptt' || msg.type === 'audio')) {
      console.log('🎤 ' + from + ' (' + phone + '): [voice message]');

      const media = await msg.downloadMedia();
      if (media && media.data) {
        const res = await fetch(STEWARD_URL + '/message', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            from,
            phone,
            message: '',
            audio_base64: media.data,
            audio_mimetype: media.mimetype || 'audio/ogg'
          })
        });
        console.log('→ Steward (voice): ' + res.status);
      }
      return;
    }

    // Text message
    const text = (msg.body || '').trim();
    if (!text) return;

    console.log('📩 ' + from + ' (' + phone + '): ' + text.substring(0, 80));

    const res = await fetch(STEWARD_URL + '/message', {
      method: 'POST',
      headers,
      body: JSON.stringify({ from, phone, message: text })
    });
    console.log('→ Steward: ' + res.status);
  } catch (err) {
    console.error('→ Steward error:', err.message);
  }
});

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
    await client.sendMessage(to, message);
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
    await client.logout();
    state.status = 'disconnected';
    state.qrData = null;
    state.connectedAt = null;
    res.json({ status: 'logged out' });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// CORS for admin panel
app.use((req, res, next) => {
  res.header('Access-Control-Allow-Origin', '*');
  res.header('Access-Control-Allow-Methods', 'GET, POST');
  res.header('Access-Control-Allow-Headers', 'Content-Type');
  next();
});

app.listen(PORT, () => {
  console.log('🌉 Bridge API listening on :' + PORT);
  client.initialize();
});
