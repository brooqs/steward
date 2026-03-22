import { useState, useEffect } from 'preact/hooks';

const CLOUD_PROVIDERS = [
  { value: 'groq', label: 'Groq', description: 'Fast inference, free tier', models: ['llama-3.3-70b-versatile', 'llama-3.1-8b-instant', 'mixtral-8x7b-32768'] },
  { value: 'openai', label: 'OpenAI', description: 'GPT-4o, most popular', models: ['gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo'] },
  { value: 'claude', label: 'Claude (Anthropic)', description: 'Best for reasoning', models: ['claude-sonnet-4-5-20241022', 'claude-haiku-4-5-20251001'] },
  { value: 'gemini', label: 'Google Gemini', description: 'Multimodal, free tier', models: ['gemini-2.0-flash', 'gemini-1.5-pro', 'gemini-1.5-flash'] },
  { value: 'openrouter', label: 'OpenRouter', description: 'Access all models', models: ['anthropic/claude-sonnet-4-5', 'openai/gpt-4o', 'meta-llama/llama-3.3-70b'] },
];

const LOCAL_MODELS = [
  { name: 'llama3.2:3b', label: 'Llama 3.2 3B', size: '2.0 GB', ram: '4 GB', speed: '⚡⚡⚡', desc: 'Ultra fast, great for daily chat', tags: ['Fast', 'Light'] },
  { name: 'llama3.2', label: 'Llama 3.2 8B', size: '4.7 GB', ram: '8 GB', speed: '⚡⚡', desc: 'Balanced quality and speed', tags: ['Recommended', 'Multilingual'] },
  { name: 'qwen2.5:7b', label: 'Qwen 2.5 7B', size: '4.4 GB', ram: '8 GB', speed: '⚡⚡', desc: 'Excellent for coding & Turkish', tags: ['Coding', 'Multilingual'] },
  { name: 'mistral', label: 'Mistral 7B', size: '4.1 GB', ram: '8 GB', speed: '⚡⚡', desc: 'Strong European language support', tags: ['General', 'European'] },
  { name: 'phi3:mini', label: 'Phi-3 Mini 3.8B', size: '2.3 GB', ram: '4 GB', speed: '⚡⚡⚡', desc: 'Microsoft, ultra-light reasoning', tags: ['Fast', 'Reasoning'] },
  { name: 'gemma2:9b', label: 'Gemma 2 9B', size: '5.4 GB', ram: '8 GB', speed: '⚡⚡', desc: 'Google, strong general purpose', tags: ['Quality', 'General'] },
];

const STEPS = ['Welcome', 'Admin', 'AI Engine', 'Setup', 'Model', 'Launch'];

export function Setup() {
  const [step, setStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [providerType, setProviderType] = useState(null); // 'local' | 'cloud'
  const [ollamaStatus, setOllamaStatus] = useState(null);
  const [installing, setInstalling] = useState(false);
  const [ollamaWaiting, setOllamaWaiting] = useState(false);
  const [ollamaWaitStart, setOllamaWaitStart] = useState(null);
  const [pulling, setPulling] = useState(false);
  const [pullProgress, setPullProgress] = useState(null);
  const [pullStatus, setPullStatus] = useState('');
  const [form, setForm] = useState({
    username: 'admin',
    password: '',
    provider: 'ollama',
    api_key: '',
    model: '',
    system_prompt: '',
  });

  const update = (key, val) => setForm(f => ({ ...f, [key]: val }));

  // Check Ollama status when entering setup step
  const checkOllama = async () => {
    try {
      const res = await fetch('/api/ollama/status');
      const data = await res.json();
      setOllamaStatus(data);
    } catch {
      setOllamaStatus({ installed: false, running: false, models: [] });
    }
  };

  useEffect(() => {
    if (step === 3 && providerType === 'local') {
      checkOllama();
      const interval = setInterval(checkOllama, 5000);
      return () => clearInterval(interval);
    }
  }, [step, providerType]);

  const handleInstallOllama = async () => {
    setInstalling(true);
    setError('');
    try {
      const res = await fetch('/api/ollama/install', { method: 'POST' });
      const data = await res.json();
      if (data.error) {
        setError(data.error);
        setInstalling(false);
      } else {
        // Switch to waiting mode — poll until Ollama is running
        setInstalling(false);
        setOllamaWaiting(true);
        setOllamaWaitStart(Date.now());
      }
    } catch {
      setError('Install failed');
      setInstalling(false);
    }
  };

  // Poll while waiting for Ollama to start
  useEffect(() => {
    if (!ollamaWaiting) return;
    const poll = setInterval(async () => {
      try {
        const res = await fetch('/api/ollama/status');
        const data = await res.json();
        setOllamaStatus(data);
        if (data.running) {
          setOllamaWaiting(false);
          clearInterval(poll);
        }
      } catch {}
    }, 3000);
    return () => clearInterval(poll);
  }, [ollamaWaiting]);

  const handlePullModel = async (model) => {
    setPulling(true);
    setPullStatus(`Downloading ${model}...`);
    setPullProgress({ status: 'starting', percent: 0 });
    try {
      const res = await fetch('/api/ollama/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model }),
      });

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          try {
            const data = JSON.parse(line.slice(6));
            if (data.status === 'success') {
              setPullStatus('✅ ' + model + ' downloaded!');
              setPullProgress(null);
              update('model', model);
              await checkOllama();
            } else if (data.total > 0) {
              const pct = Math.round((data.completed / data.total) * 100);
              setPullProgress({ status: data.status, percent: pct });
              setPullStatus(`${data.status} — ${pct}%`);
            } else {
              setPullProgress({ status: data.status, percent: 0 });
              setPullStatus(data.status);
            }
          } catch {}
        }
      }
    } catch {
      setPullStatus('❌ Download failed');
      setPullProgress(null);
    }
    setPulling(false);
  };

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
      if (!res.ok) { setError(data.error || 'Setup failed'); setSaving(false); return; }
      const poll = setInterval(async () => {
        try {
          const r = await fetch('/api/status');
          if (r.ok) { const s = await r.json(); if (!s.setup_mode) { clearInterval(poll); window.location.href = '/'; } }
        } catch {}
      }, 2000);
    } catch {
      const poll = setInterval(async () => {
        try { const r = await fetch('/api/status'); if (r.ok) { clearInterval(poll); window.location.href = '/'; } } catch {}
      }, 2000);
    }
  };

  const canNext = () => {
    if (step === 1) return form.username && form.password;
    if (step === 2) return providerType !== null;
    if (step === 3) {
      if (providerType === 'local') return ollamaStatus?.running;
      return form.provider && form.api_key;
    }
    if (step === 4) return form.model;
    return true;
  };

  const selectedCloudProvider = CLOUD_PROVIDERS.find(p => p.value === form.provider);
  const installedModels = (ollamaStatus?.models || []).map(m => m.name?.split(':')[0]);

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

        {/* Step 0: Privacy Manifesto */}
        {step === 0 && (
          <div class="setup-content">
            <div style="text-align: center; padding: 10px 0;">
              <div style="font-size: 52px; margin-bottom: 12px;">🛡️</div>
              <h1 style="margin: 0 0 8px; font-size: 26px; color: var(--text);">Your AI, Your Rules</h1>
              <p style="color: var(--text-muted); font-size: 14px; max-width: 420px; margin: 0 auto 24px;">
                Steward is built with privacy as a core principle. Your conversations, data, and personal information belong to <strong style="color: var(--text);">you</strong>.
              </p>
              <div style="display: flex; flex-direction: column; gap: 12px; text-align: left; max-width: 380px; margin: 0 auto;">
                <div class="privacy-point">
                  <span style="font-size: 20px;">🏠</span>
                  <div>
                    <strong style="color: var(--text);">Runs on your device</strong>
                    <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">No cloud servers required. Your assistant lives on your hardware.</p>
                  </div>
                </div>
                <div class="privacy-point">
                  <span style="font-size: 20px;">🔐</span>
                  <div>
                    <strong style="color: var(--text);">Zero data collection</strong>
                    <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">We never collect, store, or transmit your data. Period.</p>
                  </div>
                </div>
                <div class="privacy-point">
                  <span style="font-size: 20px;">🧠</span>
                  <div>
                    <strong style="color: var(--text);">Local AI models available</strong>
                    <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">Run AI completely offline. No data ever leaves your computer.</p>
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Step 1: Admin Credentials */}
        {step === 1 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🔐 Admin Credentials</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Secure your admin panel. You'll use these credentials to access the dashboard.
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

        {/* Step 2: Provider Type — Local vs Cloud */}
        {step === 2 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🧠 Choose Your AI Engine</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              How would you like to power your assistant?
            </p>
            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 14px;">
              <div class={`provider-card ${providerType === 'local' ? 'selected' : ''}`}
                onClick={() => { setProviderType('local'); update('provider', 'ollama'); update('api_key', 'ollama'); }}>
                <div class="provider-badge">Recommended</div>
                <div style="font-size: 36px; margin-bottom: 8px;">🔒</div>
                <strong style="font-size: 16px; color: var(--text);">Local AI</strong>
                <p style="font-size: 12px; color: var(--text-muted); margin: 8px 0 0;">
                  Everything stays on your device. Zero data leaves your computer. Powered by Ollama.
                </p>
                <div style="margin-top: 12px; display: flex; gap: 6px; flex-wrap: wrap; justify-content: center;">
                  <span class="tag tag-green">Private</span>
                  <span class="tag tag-green">Offline</span>
                  <span class="tag tag-green">Free</span>
                </div>
              </div>
              <div class={`provider-card ${providerType === 'cloud' ? 'selected' : ''}`}
                onClick={() => { setProviderType('cloud'); update('provider', 'groq'); update('api_key', ''); }}>
                <div style="font-size: 36px; margin-bottom: 8px;">☁️</div>
                <strong style="font-size: 16px; color: var(--text);">Cloud AI</strong>
                <p style="font-size: 12px; color: var(--text-muted); margin: 8px 0 0;">
                  More powerful models. Requires API key. Data sent to provider.
                </p>
                <div style="margin-top: 12px; display: flex; gap: 6px; flex-wrap: wrap; justify-content: center;">
                  <span class="tag tag-blue">Powerful</span>
                  <span class="tag tag-amber">API Key</span>
                  <span class="tag tag-amber">Cloud</span>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Step 3: Setup — Ollama or Cloud Provider */}
        {step === 3 && (
          <div class="setup-content">
            {providerType === 'local' ? (
              <div>
                <h2 style="margin: 0 0 4px;">🦙 Ollama Setup</h2>
                <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
                  Ollama runs AI models locally on your device.
                </p>
                {ollamaStatus === null ? (
                  <p style="color: var(--text-muted);">Checking Ollama status...</p>
                ) : ollamaStatus.running ? (
                  <div class="status-card status-ok">
                    <span style="font-size: 24px;">✅</span>
                    <div>
                      <strong>Ollama is running!</strong>
                      <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">
                        {(ollamaStatus.models || []).length} model(s) available
                      </p>
                    </div>
                  </div>
                ) : ollamaStatus.installed || ollamaWaiting ? (
                  <div>
                    <div class="status-card status-warn">
                      <span style="font-size: 24px;">⏳</span>
                      <div>
                        <strong>Please wait, Ollama is getting ready...</strong>
                        <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">
                          Ollama is installed and starting up. This usually takes a few seconds.
                        </p>
                      </div>
                    </div>
                    {ollamaWaitStart && (Date.now() - ollamaWaitStart) > 60000 && (
                      <div style="margin-top: 12px; padding: 10px 14px; background: var(--surface-hover); border-radius: var(--radius-sm); font-size: 12px; color: var(--text-muted);">
                        💡 It's been over a minute. You can try starting it manually:
                        <code style="display: block; margin-top: 6px; padding: 6px 10px; background: var(--bg); border-radius: 4px;">brew services start ollama</code>
                      </div>
                    )}
                  </div>
                ) : (
                  <div>
                    <div class="status-card status-info">
                      <span style="font-size: 24px;">📦</span>
                      <div>
                        <strong>Ollama not found</strong>
                        <p style="margin: 2px 0 0; font-size: 12px; color: var(--text-muted);">
                          Install it to run AI models locally.
                        </p>
                      </div>
                    </div>
                    <button class="btn-primary" onClick={handleInstallOllama} disabled={installing}
                      style="margin-top: 16px; width: 100%;">
                      {installing ? '⏳ Installing Ollama...' : '📥 Install Ollama via Homebrew'}
                    </button>
                    <p style="text-align: center; margin-top: 8px; font-size: 11px; color: var(--text-muted);">
                      Or install manually from <a href="https://ollama.com" target="_blank" style="color: var(--accent);">ollama.com</a>
                    </p>
                  </div>
                )}
              </div>
            ) : (
              <div>
                <h2 style="margin: 0 0 4px;">☁️ Cloud Provider</h2>
                <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
                  Choose your cloud AI provider. You can change this later in Settings.
                </p>
                <div class="form-group">
                  <label>Provider</label>
                  <select value={form.provider} onChange={e => { update('provider', e.target.value); update('model', ''); }}>
                    {CLOUD_PROVIDERS.map(p => <option key={p.value} value={p.value}>{p.label} — {p.description}</option>)}
                  </select>
                </div>
                <div class="form-group">
                  <label>API Key</label>
                  <input type="password" value={form.api_key} onInput={e => update('api_key', e.target.value)}
                    placeholder="Paste your API key" autoFocus />
                </div>
              </div>
            )}
          </div>
        )}

        {/* Step 4: Model Selection */}
        {step === 4 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">{providerType === 'local' ? '🧠 Choose a Model' : '🧠 Select Model'}</h2>
            <p style="color: var(--text-muted); margin: 0 0 16px; font-size: 13px;">
              {providerType === 'local'
                ? 'Select a model to run on your device. Smaller models are faster and use less RAM.'
                : 'Choose which model to use. You can change this later.'}
            </p>

            {providerType === 'local' ? (
              <div class="model-grid">
                {LOCAL_MODELS.map(m => {
                  const isInstalled = installedModels.includes(m.name.split(':')[0]);
                  const isSelected = form.model === m.name;
                  return (
                    <div key={m.name} class={`model-card ${isSelected ? 'selected' : ''}`}
                      onClick={() => isInstalled ? update('model', m.name) : null}>
                      <div style="display: flex; justify-content: space-between; align-items: center;">
                        <strong style="font-size: 13px; color: var(--text);">{m.label}</strong>
                        <span style="font-size: 11px; color: var(--text-muted);">{m.size}</span>
                      </div>
                      <p style="margin: 4px 0; font-size: 11px; color: var(--text-muted);">{m.desc}</p>
                      <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 6px;">
                        <div style="display: flex; gap: 4px;">
                          {m.tags.map(t => <span key={t} class="tag tag-small">{t}</span>)}
                        </div>
                        <span style="font-size: 11px;">{m.speed}</span>
                      </div>
                      <div style="margin-top: 8px;">
                        {isInstalled ? (
                          <span style="font-size: 11px; color: var(--success);">✅ Ready to use</span>
                        ) : (
                          <button class="btn-small" onClick={(e) => { e.stopPropagation(); handlePullModel(m.name); }}
                            disabled={pulling}>
                            {pulling && pullStatus.includes(m.name) ? '⏳' : '📥'} Download
                          </button>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div class="form-group">
                <label>Model</label>
                <select value={form.model} onChange={e => update('model', e.target.value)}>
                  <option value="">Default</option>
                  {selectedCloudProvider && selectedCloudProvider.models.map(m => <option key={m} value={m}>{m}</option>)}
                </select>
              </div>
            )}

            {pullStatus && (
              <div style="margin-top: 12px; padding: 10px 14px; background: var(--surface-hover); border-radius: var(--radius-sm); font-size: 12px; color: var(--text-muted);">
                {pullStatus}
                {pullProgress && pullProgress.percent > 0 && (
                  <div class="progress-bar">
                    <div class="progress-fill" style={`width: ${pullProgress.percent}%`}></div>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {/* Step 5: Confirm */}
        {step === 5 && (
          <div class="setup-content">
            <h2 style="margin: 0 0 4px;">🚀 Ready to Launch</h2>
            <p style="color: var(--text-muted); margin: 0 0 20px; font-size: 13px;">
              Review your settings and start Steward.
            </p>
            <div class="setup-summary">
              <div class="summary-row"><span>Admin</span><strong>{form.username}</strong></div>
              <div class="summary-row"><span>Engine</span><strong>{providerType === 'local' ? '🔒 Local (Ollama)' : '☁️ Cloud'}</strong></div>
              <div class="summary-row"><span>Provider</span><strong>{providerType === 'local' ? 'Ollama' : CLOUD_PROVIDERS.find(p => p.value === form.provider)?.label}</strong></div>
              <div class="summary-row"><span>Model</span><strong>{form.model || 'Default'}</strong></div>
              <div class="summary-row"><span>Privacy</span><strong style={providerType === 'local' ? 'color: var(--success);' : 'color: var(--warning);'}>
                {providerType === 'local' ? '🔒 Full — data stays local' : '⚠️ Data sent to cloud provider'}
              </strong></div>
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
          {step < 5 ? (
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
          max-width: 580px;
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
          font-size: 10px;
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
          min-height: 240px;
        }
        .setup-content h2 {
          font-size: 20px;
          color: var(--text);
        }
        .privacy-point {
          display: flex;
          gap: 12px;
          align-items: flex-start;
          padding: 10px 14px;
          background: var(--surface-hover);
          border-radius: var(--radius-sm);
        }
        .provider-card {
          padding: 20px 16px;
          background: var(--surface-hover);
          border: 2px solid var(--border);
          border-radius: 12px;
          text-align: center;
          cursor: pointer;
          transition: all 0.2s;
          position: relative;
        }
        .provider-card:hover { border-color: var(--accent); }
        .provider-card.selected {
          border-color: var(--accent);
          background: rgba(99, 102, 241, 0.08);
          box-shadow: 0 0 0 1px var(--accent);
        }
        .provider-badge {
          position: absolute;
          top: -10px;
          left: 50%;
          transform: translateX(-50%);
          background: var(--success);
          color: white;
          font-size: 10px;
          font-weight: 700;
          padding: 2px 10px;
          border-radius: 10px;
          text-transform: uppercase;
          letter-spacing: 0.5px;
        }
        .tag {
          font-size: 10px;
          padding: 2px 8px;
          border-radius: 10px;
          font-weight: 600;
        }
        .tag-green { background: rgba(34,197,94,0.15); color: #22c55e; }
        .tag-blue { background: rgba(59,130,246,0.15); color: #3b82f6; }
        .tag-amber { background: rgba(245,158,11,0.15); color: #f59e0b; }
        .tag-small { font-size: 9px; padding: 1px 6px; background: var(--surface-hover); color: var(--text-muted); }
        .status-card {
          display: flex;
          gap: 12px;
          align-items: center;
          padding: 14px;
          border-radius: var(--radius-sm);
        }
        .status-ok { background: rgba(34,197,94,0.08); border: 1px solid rgba(34,197,94,0.2); }
        .status-warn { background: rgba(245,158,11,0.08); border: 1px solid rgba(245,158,11,0.2); }
        .status-info { background: var(--surface-hover); border: 1px solid var(--border); }
        .model-grid {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 10px;
        }
        .model-card {
          padding: 12px;
          background: var(--surface-hover);
          border: 2px solid var(--border);
          border-radius: 10px;
          cursor: pointer;
          transition: all 0.2s;
        }
        .model-card:hover { border-color: var(--accent); }
        .model-card.selected {
          border-color: var(--accent);
          background: rgba(99,102,241,0.08);
        }
        .progress-bar {
          height: 6px;
          background: var(--border);
          border-radius: 3px;
          margin-top: 8px;
          overflow: hidden;
        }
        .progress-fill {
          height: 100%;
          background: linear-gradient(90deg, var(--accent), #818cf8);
          border-radius: 3px;
          transition: width 0.3s ease;
        }
        .btn-small {
          background: var(--accent);
          color: white;
          border: none;
          padding: 4px 12px;
          border-radius: 6px;
          font-size: 11px;
          cursor: pointer;
          font-weight: 600;
        }
        .btn-small:disabled { opacity: 0.5; cursor: not-allowed; }
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
