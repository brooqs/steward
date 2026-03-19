import { useState, useEffect } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Channels() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  useEffect(() => {
    fetch('/api/config')
      .then(r => r.json())
      .then(data => { setConfig(data); setLoading(false); })
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

      <div class="card">
        <div class="card-title">WhatsApp</div>
        <p style="font-size: 12px; color: var(--text-muted); margin-bottom: 16px;">
          WhatsApp uses a bridge server. Configure the bridge URL and webhook settings below.
        </p>
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
      </div>

      <button class="btn btn-primary" onClick={save} disabled={saving}>
        {saving ? '⏳ Saving...' : '💾 Save Channel Settings'}
      </button>
    </div>
  );
}
