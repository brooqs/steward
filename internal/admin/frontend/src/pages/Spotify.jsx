import { useState } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Spotify() {
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const [callbackUrl, setCallbackUrl] = useState('');
  const [step, setStep] = useState(1); // 1=credentials, 2=callback
  const toast = useToast();

  const startAuth = async () => {
    if (!clientId || !clientSecret) {
      toast('Client ID and Secret are required', 'error');
      return;
    }
    try {
      const res = await fetch('/api/spotify/authorize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ client_id: clientId, client_secret: clientSecret }),
      });
      const data = await res.json();
      if (data.auth_url) {
        window.open(data.auth_url, '_blank');
        setStep(2);
        toast('Authorization page opened. Complete the flow, then paste the callback URL below.', 'success');
      } else {
        toast(data.error || 'Failed to get auth URL', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
  };

  const exchangeToken = async () => {
    if (!callbackUrl) {
      toast('Paste the callback URL first', 'error');
      return;
    }
    try {
      const res = await fetch('/api/spotify/exchange', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ callback_url: callbackUrl }),
      });
      const data = await res.json();
      if (data.message) {
        toast(data.message, 'success');
        setTimeout(() => { window.location.href = '/integrations'; }, 1500);
      } else {
        toast(data.error || 'Token exchange failed', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
  };

  return (
    <div>
      <h2 class="page-title">🎵 Spotify Setup</h2>

      <div class="card">
        <div class="card-title">OAuth2 Configuration</div>
        <ol class="step-list">
          <li>Go to <a href="https://developer.spotify.com/dashboard" target="_blank">Spotify Developer Dashboard</a></li>
          <li>Create an app and get Client ID / Client Secret</li>
          <li>Add redirect URI: <code>http://127.0.0.1:8888/callback</code></li>
          <li>Enter credentials below, click Authorize, then paste the callback URL</li>
        </ol>
      </div>

      {/* Step 1: Credentials */}
      <div class="card">
        <div class="card-title">Step 1 — Credentials {step > 1 && '✅'}</div>
        <div class="form-group">
          <label>Client ID</label>
          <input type="text" value={clientId} onInput={e => setClientId(e.target.value)} placeholder="Enter Spotify Client ID" disabled={step > 1} />
        </div>
        <div class="form-group">
          <label>Client Secret</label>
          <input type="password" value={clientSecret} onInput={e => setClientSecret(e.target.value)} placeholder="Enter Spotify Client Secret" disabled={step > 1} />
        </div>
        {step === 1 && (
          <button class="btn btn-primary" onClick={startAuth}>🔗 Authorize with Spotify</button>
        )}
        {step > 1 && (
          <button class="btn" style="font-size: 12px; opacity: 0.6;" onClick={() => setStep(1)}>← Re-enter credentials</button>
        )}
      </div>

      {/* Step 2: Callback URL */}
      {step >= 2 && (
        <div class="card">
          <div class="card-title">Step 2 — Paste Callback URL</div>
          <p style="color: var(--text-muted); font-size: 13px; margin: 0 0 12px;">
            After authorizing on Spotify, you'll be redirected to a URL starting with <code>http://127.0.0.1:8888/callback?code=...</code>. 
            Copy that entire URL and paste it below.
          </p>
          <div class="form-group">
            <label>Callback URL</label>
            <input type="text" value={callbackUrl} onInput={e => setCallbackUrl(e.target.value)} 
              placeholder="http://127.0.0.1:8888/callback?code=..." autoFocus />
          </div>
          <button class="btn btn-primary" onClick={exchangeToken}>✅ Complete Setup</button>
        </div>
      )}
    </div>
  );
}
