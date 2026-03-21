import { useState, useEffect, useRef } from 'preact/hooks';
import { useToast } from '../components/Toast';

const STATUS_LABELS = {
  initializing: { text: 'Initializing...', badge: 'badge-warning', icon: '⏳' },
  qr: { text: 'Waiting for QR scan', badge: 'badge-warning', icon: '📱' },
  authenticated: { text: 'Authenticated', badge: 'badge-success', icon: '🔐' },
  ready: { text: 'Connected', badge: 'badge-success', icon: '✅' },
  disconnected: { text: 'Disconnected', badge: 'badge-error', icon: '❌' },
};

function WhatsAppBridge({ toast }) {
  const [bridgeStatus, setBridgeStatus] = useState(null);
  const [qrUrl, setQrUrl] = useState(null);
  const [bridgeError, setBridgeError] = useState(false);
  const [serviceStatus, setServiceStatus] = useState(null);
  const [serviceLoading, setServiceLoading] = useState(false);
  const intervalRef = useRef(null);

  const pollBridge = async () => {
    // Check service status
    try {
      const svcRes = await fetch('/api/whatsapp/bridge/service');
      const svcData = await svcRes.json();
      setServiceStatus(svcData);
    } catch {}

    // Check bridge health
    try {
      const res = await fetch('/api/whatsapp/health');
      const data = await res.json();

      if (data.error) {
        setBridgeError(true);
        setBridgeStatus(null);
        return;
      }

      setBridgeStatus(data);
      setBridgeError(false);

      if (data.status === 'qr') {
        const qrRes = await fetch('/api/whatsapp/qr');
        const qrData = await qrRes.json();
        if (qrData.available && qrData.data) {
          setQrUrl(`https://api.qrserver.com/v1/create-qr-code/?size=280x280&data=${encodeURIComponent(qrData.data)}`);
        }
      } else {
        setQrUrl(null);
      }
    } catch {
      setBridgeError(true);
      setBridgeStatus(null);
    }
  };

  useEffect(() => {
    pollBridge();
    intervalRef.current = setInterval(pollBridge, 3000);
    return () => clearInterval(intervalRef.current);
  }, []);

  const handleLogout = async () => {
    if (!confirm('Disconnect WhatsApp session? You will need to scan QR again.')) return;
    try {
      await fetch('/api/whatsapp/logout', { method: 'POST' });
      toast('WhatsApp session disconnected', 'success');
      pollBridge();
    } catch {
      toast('Failed to logout', 'error');
    }
  };

  const handleService = async (action) => {
    setServiceLoading(true);
    try {
      const res = await fetch('/api/whatsapp/bridge/service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      const data = await res.json();
      if (data.error) {
        toast(data.error, 'error');
      } else {
        toast(data.message, 'success');
      }
      setTimeout(pollBridge, 2000);
    } catch {
      toast('Service action failed', 'error');
    }
    setServiceLoading(false);
  };

  const statusInfo = bridgeStatus ? (STATUS_LABELS[bridgeStatus.status] || STATUS_LABELS.disconnected) : null;

  return (
    <div class="card">
      <div class="card-title">WhatsApp Bridge</div>

      {bridgeError ? (
        <div style="padding: 16px; text-align: center;">
          <p style="color: var(--text-muted); font-size: 14px; margin-bottom: 12px;">
            WhatsApp bridge is not running.
          </p>
          {serviceStatus && !serviceStatus.running ? (
            <button class="btn btn-primary" onClick={() => handleService('start')} disabled={serviceLoading}
              style="display: inline-flex; align-items: center; gap: 8px;">
              {serviceLoading ? '⏳ Starting...' : '🚀 Initialize WhatsApp Service'}
            </button>
          ) : serviceStatus && serviceStatus.running ? (
            <div>
              <p style="font-size: 12px; color: var(--text-muted); margin-bottom: 8px;">
                Service is starting up — waiting for bridge to respond...
              </p>
              <button class="btn btn-outline" onClick={() => handleService('stop')} disabled={serviceLoading}
                style="font-size: 12px; border-color: var(--error); color: var(--error);">
                Stop Service
              </button>
            </div>
          ) : (
            <button class="btn btn-primary" onClick={() => handleService('start')} disabled={serviceLoading}
              style="display: inline-flex; align-items: center; gap: 8px;">
              {serviceLoading ? '⏳ Starting...' : '🚀 Initialize WhatsApp Service'}
            </button>
          )}
        </div>
      ) : bridgeStatus ? (
        <div>
          <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 16px;">
            <span style="font-size: 24px;">{statusInfo.icon}</span>
            <div>
              <span class={`badge ${statusInfo.badge}`}>{statusInfo.text}</span>
              {bridgeStatus.connectedAt && (
                <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">
                  Connected since: {new Date(bridgeStatus.connectedAt).toLocaleString()}
                </div>
              )}
            </div>
            {bridgeStatus.messageCount > 0 && (
              <span style="margin-left: auto; font-size: 12px; color: var(--text-muted);">
                {bridgeStatus.messageCount} messages processed
              </span>
            )}
          </div>

          {bridgeStatus.status === 'qr' && qrUrl && (
            <div style="text-align: center; padding: 20px; background: white; border-radius: var(--radius-sm); margin-bottom: 16px;">
              <img src={qrUrl} alt="WhatsApp QR Code" style="width: 280px; height: 280px;" />
              <p style="margin-top: 12px; font-size: 13px; color: #333;">
                📱 Open WhatsApp → Settings → Linked Devices → Link a Device
              </p>
            </div>
          )}

          <div style="display: flex; gap: 8px;">
            {bridgeStatus.status === 'ready' && (
              <button class="btn btn-outline" style="border-color: var(--error); color: var(--error);" onClick={handleLogout}>
                🔌 Disconnect Session
              </button>
            )}
            {serviceStatus && serviceStatus.running && (
              <button class="btn btn-outline" onClick={() => handleService('stop')} disabled={serviceLoading}
                style="font-size: 12px;">
                ⏹ Stop Service
              </button>
            )}
          </div>
        </div>
      ) : (
        <p style="color: var(--text-muted);">Loading bridge status...</p>
      )}
    </div>
  );
}

export function Channels() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  useEffect(() => {
    fetch('/api/config')
      .then(r => r.json())
      .then(data => { setConfig(data.config || data); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/config/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      if (res.ok) {
        toast('Channel settings saved! Restart required.', 'success');
      } else {
        toast('Failed to save', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
    setSaving(false);
  };

  const update = (parent, key, value) => {
    setConfig(prev => ({
      ...prev,
      [parent]: { ...prev[parent], [key]: value }
    }));
  };

  if (loading) return <div class="page-title">Loading...</div>;
  if (!config) return <div class="page-title">Failed to load config</div>;

  const tg = config.telegram || {};
  const wa = config.whatsapp || {};

  return (
    <div>
      <h2 class="page-title">💬 Channels</h2>

      {/* WhatsApp Bridge Status + QR */}
      <WhatsAppBridge toast={toast} />

      <div class="card">
        <div class="card-title">WhatsApp Configuration</div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
          <div class="form-group">
            <label>Listen Address</label>
            <input type="text" value={wa.listen_addr || ''} onInput={e => update('whatsapp', 'listen_addr', e.target.value)} placeholder="0.0.0.0:8765" />
          </div>
          <div class="form-group">
            <label>Bridge URL</label>
            <input type="url" value={wa.bridge_url || ''} onInput={e => update('whatsapp', 'bridge_url', e.target.value)} placeholder="http://localhost:3000" />
          </div>
        </div>
        <div class="form-group">
          <label>Webhook Secret</label>
          <input type="password" value={wa.webhook_secret || ''} onInput={e => update('whatsapp', 'webhook_secret', e.target.value)} placeholder="${WHATSAPP_WEBHOOK_SECRET}" />
        </div>
        <div class="form-group">
          <label>Allowed Phone Numbers (comma separated)</label>
          <input type="text" value={(wa.allowed_ids || []).join(', ')} onInput={e => update('whatsapp', 'allowed_ids', e.target.value.split(',').map(s => s.trim()).filter(s => s))} placeholder="905xxxxxxxxxx, 905yyyyyyyyyy" />
          <span style="font-size: 11px; color: var(--text-muted);">Leave empty to allow anyone (⚠️ not recommended)</span>
        </div>
      </div>

      <div class="card">
        <div class="card-title">Telegram</div>
        <p style="font-size: 12px; color: var(--text-muted); margin-bottom: 16px;">
          Get your bot token from <a href="https://t.me/BotFather" target="_blank">@BotFather</a> on Telegram.
        </p>
        <div class="form-group">
          <label>Bot Token</label>
          <input type="password" value={tg.token || ''} onInput={e => update('telegram', 'token', e.target.value)} placeholder="${TELEGRAM_BOT_TOKEN}" />
        </div>
        <div class="form-group">
          <label>Allowed User/Chat IDs (comma separated)</label>
          <input type="text" value={(tg.allowed_ids || []).join(', ')} onInput={e => update('telegram', 'allowed_ids', e.target.value.split(',').map(s => parseInt(s.trim())).filter(n => !isNaN(n)))} placeholder="123456789, 987654321" />
          <span style="font-size: 11px; color: var(--text-muted);">Leave empty to allow anyone (⚠️ not recommended)</span>
        </div>
      </div>

      <button class="btn btn-primary" onClick={save} disabled={saving}>
        {saving ? '⏳ Saving...' : '💾 Save Channel Settings'}
      </button>
    </div>
  );
}
