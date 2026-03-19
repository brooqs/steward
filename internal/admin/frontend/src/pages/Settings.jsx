import { useState, useEffect } from 'preact/hooks';
import { useToast } from '../components/Toast';

const PROVIDERS = ['groq', 'openai', 'claude', 'gemini', 'ollama', 'openrouter'];
const MEMORY_BACKENDS = ['badger', 'postgres'];

function EmbeddingCard() {
  const [status, setStatus] = useState(null);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const fetchStatus = () => {
    fetch('/api/embedding/status').then(r => r.json()).then(setStatus).catch(() => {});
  };

  useEffect(() => { fetchStatus(); const t = setInterval(fetchStatus, 3000); return () => clearInterval(t); }, []);

  const doAction = async (action, provider) => {
    setBusy(true);
    try {
      const res = await fetch('/api/embedding/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action, provider }),
      });
      const data = await res.json();
      toast(data.message || 'Done', res.ok ? 'success' : 'error');
      if (action === 'enable_hf' || action === 'enable') {
        const poll = setInterval(async () => {
          try { const r = await fetch('/api/status'); if (r.ok) { clearInterval(poll); window.location.reload(); } } catch {}
        }, 2000);
      }
      fetchStatus();
    } catch { toast('Network error', 'error'); }
    setBusy(false);
  };

  if (!status) return null;

  return (
    <div class="card">
      <div class="card-title">🧠 Smart Tool Selection</div>
      <p style="color: var(--text-muted); font-size: 13px; margin: 0 0 16px;">
        Uses AI embeddings to select the most relevant tools for each message, improving accuracy and reducing token usage.
      </p>

      {status.embedding_enabled ? (
        <div style="padding: 12px 16px; background: rgba(34,197,94,0.1); border: 1px solid #22c55e; border-radius: var(--radius-sm); margin-bottom: 12px;">
          <strong style="color: #22c55e;">✅ Active</strong>
          <span style="color: var(--text-muted); margin-left: 12px;">Provider: {status.embedding_provider}</span>
        </div>
      ) : (
        <div style="display: flex; flex-direction: column; gap: 10px;">
          <button class="btn btn-primary" onClick={() => doAction('enable_hf')} disabled={busy}
            style="display: flex; align-items: center; gap: 8px; justify-content: center;">
            {busy ? '⏳' : '🤗'} Enable with HuggingFace (free, no setup)
          </button>
          <div style="display: flex; gap: 8px;">
            <button class="btn" onClick={() => doAction('download')} disabled={busy || status.downloading}
              style="flex: 1; font-size: 12px; background: var(--surface-hover); border: 1px solid var(--border); color: var(--text-muted); padding: 8px; border-radius: var(--radius-sm); cursor: pointer;">
              📥 Download ONNX Model ({status.model_downloaded ? '✅' : '~23MB'})
            </button>
            <button class="btn" onClick={() => doAction('enable', 'ollama')} disabled={busy}
              style="flex: 1; font-size: 12px; background: var(--surface-hover); border: 1px solid var(--border); color: var(--text-muted); padding: 8px; border-radius: var(--radius-sm); cursor: pointer;">
              🦙 Use Ollama (local)
            </button>
          </div>
        </div>
      )}

      {status.downloading && status.progress && (
        <div style="margin-top: 10px; padding: 8px 12px; background: var(--surface-hover); border-radius: var(--radius-sm); font-size: 12px; color: var(--accent);">
          ⏳ {status.progress}
        </div>
      )}
    </div>
  );
}

export function Settings() {
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

  const saveConfig = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/config/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      if (res.ok) {
        toast('Settings saved! Restart may be needed for some changes.', 'success');
      } else {
        toast('Failed to save settings', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
    setSaving(false);
  };

  const update = (key, value) => {
    setConfig(prev => ({ ...prev, [key]: value }));
  };

  const updateNested = (parent, key, value) => {
    setConfig(prev => ({
      ...prev,
      [parent]: { ...prev[parent], [key]: value }
    }));
  };

  if (loading) return <div class="page-title">Loading...</div>;
  if (!config) return <div class="page-title">Failed to load config</div>;

  return (
    <div>
      <h2 class="page-title">⚙️ Settings</h2>

      <div class="card">
        <div class="card-title">AI Provider</div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
          <div class="form-group">
            <label>Provider</label>
            <select value={config.provider} onChange={e => update('provider', e.target.value)}>
              {PROVIDERS.map(p => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div class="form-group">
            <label>Model</label>
            <input type="text" value={config.model || ''} onInput={e => update('model', e.target.value)} placeholder="llama-3.3-70b-versatile" />
          </div>
          <div class="form-group">
            <label>API Key</label>
            <input type="password" value={config.api_key || ''} onInput={e => update('api_key', e.target.value)} placeholder="sk-..." />
          </div>
          <div class="form-group">
            <label>Base URL (optional)</label>
            <input type="url" value={config.base_url || ''} onInput={e => update('base_url', e.target.value)} placeholder="Custom endpoint" />
          </div>
          <div class="form-group">
            <label>Max Tokens</label>
            <input type="text" value={config.max_tokens || ''} onInput={e => update('max_tokens', parseInt(e.target.value) || 0)} placeholder="8192" />
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">System Prompt</div>
        <div class="form-group">
          <textarea rows="6" value={config.system_prompt || ''} onInput={e => update('system_prompt', e.target.value)} placeholder="You are a helpful AI assistant named Steward..." />
        </div>
      </div>

      <div class="card">
        <div class="card-title">Memory</div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
          <div class="form-group">
            <label>Backend</label>
            <select value={(config.memory || {}).backend || 'badger'} onChange={e => updateNested('memory', 'backend', e.target.value)}>
              {MEMORY_BACKENDS.map(b => <option key={b} value={b}>{b}</option>)}
            </select>
          </div>
          <div class="form-group">
            <label>Short-Term Limit</label>
            <input type="text" value={(config.memory || {}).short_term_limit || ''} onInput={e => updateNested('memory', 'short_term_limit', parseInt(e.target.value) || 0)} placeholder="20" />
          </div>
        </div>
      </div>

      <EmbeddingCard />

      <div class="card">
        <div class="card-title">Shell Tool</div>
        <div class="form-group" style="display: flex; align-items: center; gap: 12px;">
          <label style="margin: 0;">Enabled</label>
          <label class="toggle">
            <input type="checkbox" checked={(config.shell || {}).enabled} onChange={e => updateNested('shell', 'enabled', e.target.checked)} />
            <span class="toggle-slider"></span>
          </label>
        </div>
        {(config.shell || {}).enabled && (
          <div class="form-group">
            <label>Blocked Commands (comma separated)</label>
            <input type="text" value={((config.shell || {}).blocked_commands || []).join(', ')} onInput={e => updateNested('shell', 'blocked_commands', e.target.value.split(',').map(s => s.trim()).filter(Boolean))} />
          </div>
        )}
      </div>

      <button class="btn btn-primary" onClick={saveConfig} disabled={saving}>
        {saving ? '⏳ Saving...' : '💾 Save Settings'}
      </button>
    </div>
  );
}
