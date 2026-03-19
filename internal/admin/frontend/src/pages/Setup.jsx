import { useState } from 'preact/hooks';

const PROVIDERS = [
  { value: 'claude', label: 'Claude (Anthropic)', models: ['claude-sonnet-4-5-20241022', 'claude-haiku-4-5-20251001', 'claude-3-opus-20240229'] },
  { value: 'openai', label: 'OpenAI', models: ['gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo'] },
  { value: 'groq', label: 'Groq', models: ['llama-3.3-70b-versatile', 'llama-3.1-8b-instant', 'mixtral-8x7b-32768'] },
  { value: 'gemini', label: 'Google Gemini', models: ['gemini-2.0-flash', 'gemini-1.5-pro', 'gemini-1.5-flash'] },
  { value: 'ollama', label: 'Ollama (Local)', models: ['llama3.2', 'mistral', 'codellama'] },
  { value: 'openrouter', label: 'OpenRouter', models: ['anthropic/claude-sonnet-4-5', 'openai/gpt-4o', 'meta-llama/llama-3.3-70b'] },
];

const STEPS = ['Welcome', 'Admin', 'Provider', 'Extras', 'Confirm'];

export function Setup() {
  const [step, setStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [form, setForm] = useState({
    username: 'admin',
    password: '',
    provider: 'claude',
    api_key: '',
    model: '',
    system_prompt: '',
  });

  const update = (key, val) => setForm(f => ({ ...f, [key]: val }));
  const selectedProvider = PROVIDERS.find(p => p.value === form.provider);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      });
      const data = await res.json();
      if (!res.ok) {
        setError(data.error || 'Setup failed');
        setSaving(false);
        return;
      }
      // Poll until service is back in normal mode
      const poll = setInterval(async () => {
        try {
          const r = await fetch('/api/status');
          if (r.ok) {
            const s = await r.json();
            if (!s.setup_mode) {
              clearInterval(poll);
              window.location.href = '/';
            }
          }
        } catch {}
      }, 2000);
    } catch (e) {
      // Expected — server is restarting
      const poll = setInterval(async () => {
        try {
          const r = await fetch('/api/status');
          if (r.ok) {
            clearInterval(poll);
            window.location.href = '/';
          }
        } catch {}
      }, 2000);
    }
  };

  const canNext = () => {
    if (step === 1) return form.username && form.password;
    if (step === 2) return form.provider && form.api_key;
    return true;
  };

  return (
    <div class="setup-container">
      <div class="setup-card">
        {/* Progress */}
        <div class="setup-progress">
          {STEPS.map((s, i) => (
            <div key={s} class={`setup-step ${i === step ? 'active' : ''} ${i < step ? 'done' : ''}`}>
              <div class="step-dot">{i < step ? '✓' : i + 1}</div>
              <span>{s}</span>
            </div>
          ))}
        </div>

        {/* Step 0: Welcome */}
        {step === 0 && (
          <div class="setup-content">
            <div style="text-align: center; padding: 20px 0;">
              <div style="font-size: 48px; margin-bottom: 16px;">🤖</div>
              <h1 style="margin: 0 0 8px; font-size: 28px; color: var(--text);">Welcome to Steward</h1>
              <p style="color: var(--text-muted); font-size: 15px; max-width: 400px; margin: 0 auto;">
                Your AI personal assistant is almost ready. Let's set up the basics in a few quick steps.
              </p>
            </div>
          </div>
        )}

        {/* Step 1: Admin Credentials */}
        {step === 1 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🔐 Admin Credentials</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Set up your admin panel login. You'll use these to access the dashboard.
            </p>
            <div class="form-group">
              <label>Username</label>
              <input type="text" value={form.username} onInput={e => update('username', e.target.value)} placeholder="admin" />
            </div>
            <div class="form-group">
              <label>Password</label>
              <input type="password" value={form.password} onInput={e => update('password', e.target.value)} placeholder="Choose a strong password" autoFocus />
            </div>
          </div>
        )}

        {/* Step 2: Provider */}
        {step === 2 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🧠 AI Provider</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Choose which AI provider to use. You can change this later in Settings.
            </p>
            <div class="form-group">
              <label>Provider</label>
              <select value={form.provider} onChange={e => { update('provider', e.target.value); update('model', ''); }}>
                {PROVIDERS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
              </select>
            </div>
            <div class="form-group">
              <label>API Key</label>
              <input type="password" value={form.api_key} onInput={e => update('api_key', e.target.value)} 
                placeholder={form.provider === 'ollama' ? 'Not needed for Ollama (enter any value)' : 'Paste your API key'} autoFocus />
            </div>
            <div class="form-group">
              <label>Model <span style="color: var(--text-muted); font-size: 11px;">(leave empty for default)</span></label>
              <select value={form.model} onChange={e => update('model', e.target.value)}>
                <option value="">Default</option>
                {selectedProvider && selectedProvider.models.map(m => <option key={m} value={m}>{m}</option>)}
              </select>
            </div>
            {form.provider === 'ollama' && (
              <div style="padding: 10px 14px; background: var(--surface-hover); border-radius: var(--radius-sm); font-size: 12px; color: var(--text-muted);">
                💡 Make sure Ollama is running locally. Set <code>base_url</code> in Settings if it's on another machine.
              </div>
            )}
          </div>
        )}

        {/* Step 3: Extras */}
        {step === 3 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">✨ Personalization</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Optional — customize your assistant's personality. Leave empty for defaults.
            </p>
            <div class="form-group">
              <label>System Prompt</label>
              <textarea rows={5} value={form.system_prompt}
                onInput={e => update('system_prompt', e.target.value)}
                placeholder="You are Steward, a helpful AI personal assistant..."></textarea>
            </div>
          </div>
        )}

        {/* Step 4: Confirm */}
        {step === 4 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🚀 Ready to Launch</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Review your settings and click Start to initialize Steward.
            </p>
            <div class="setup-summary">
              <div class="summary-row"><span>Admin</span><strong>{form.username}</strong></div>
              <div class="summary-row"><span>Provider</span><strong>{PROVIDERS.find(p => p.value === form.provider)?.label}</strong></div>
              <div class="summary-row"><span>Model</span><strong>{form.model || 'Default'}</strong></div>
              <div class="summary-row"><span>System Prompt</span><strong>{form.system_prompt ? 'Custom' : 'Default'}</strong></div>
            </div>
            {error && <div style="color: var(--danger); margin-top: 12px; font-size: 13px;">❌ {error}</div>}
          </div>
        )}

        {/* Navigation */}
        <div class="setup-nav">
          {step > 0 && (
            <button class="btn-secondary" onClick={() => setStep(s => s - 1)} disabled={saving}>
              ← Back
            </button>
          )}
          <div style="flex: 1;"></div>
          {step < 4 ? (
            <button class="btn-primary" onClick={() => setStep(s => s + 1)} disabled={!canNext()}>
              {step === 0 ? "Let's Go →" : 'Next →'}
            </button>
          ) : (
            <button class="btn-primary" onClick={handleSave} disabled={saving}
              style={saving ? 'opacity: 0.7;' : ''}>
              {saving ? '⏳ Starting Steward...' : '🚀 Start Steward'}
            </button>
          )}
        </div>
      </div>

      <style>{`
        .setup-container {
          display: flex;
          align-items: center;
          justify-content: center;
          min-height: 100vh;
          padding: 20px;
          background: var(--bg);
        }
        .setup-card {
          background: var(--surface);
          border: 1px solid var(--border);
          border-radius: 16px;
          padding: 32px;
          width: 100%;
          max-width: 520px;
          box-shadow: 0 8px 32px rgba(0,0,0,0.3);
        }
        .setup-progress {
          display: flex;
          gap: 4px;
          margin-bottom: 28px;
          justify-content: center;
        }
        .setup-step {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 4px;
          font-size: 11px;
          color: var(--text-muted);
          flex: 1;
        }
        .setup-step.active { color: var(--accent); }
        .setup-step.done { color: var(--success); }
        .step-dot {
          width: 28px;
          height: 28px;
          border-radius: 50%;
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 12px;
          font-weight: 600;
          background: var(--surface-hover);
          border: 2px solid var(--border);
          transition: all 0.2s;
        }
        .setup-step.active .step-dot {
          background: var(--accent);
          border-color: var(--accent);
          color: white;
        }
        .setup-step.done .step-dot {
          background: var(--success);
          border-color: var(--success);
          color: white;
        }
        .setup-content {
          min-height: 200px;
        }
        .setup-content h2 {
          font-size: 20px;
          color: var(--text);
        }
        .setup-summary {
          display: flex;
          flex-direction: column;
          gap: 8px;
        }
        .summary-row {
          display: flex;
          justify-content: space-between;
          padding: 10px 14px;
          background: var(--surface-hover);
          border-radius: var(--radius-sm);
          font-size: 13px;
        }
        .summary-row span { color: var(--text-muted); }
        .summary-row strong { color: var(--text); }
        .setup-nav {
          display: flex;
          gap: 12px;
          margin-top: 24px;
          padding-top: 20px;
          border-top: 1px solid var(--border);
        }
        .btn-primary {
          background: var(--accent);
          color: white;
          border: none;
          padding: 10px 24px;
          border-radius: var(--radius-sm);
          cursor: pointer;
          font-size: 14px;
          font-weight: 600;
          transition: opacity 0.2s;
        }
        .btn-primary:hover { opacity: 0.9; }
        .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
        .btn-secondary {
          background: var(--surface-hover);
          color: var(--text-muted);
          border: 1px solid var(--border);
          padding: 10px 20px;
          border-radius: var(--radius-sm);
          cursor: pointer;
          font-size: 14px;
        }
        .btn-secondary:hover { color: var(--text); }
      `}</style>
    </div>
  );
}
