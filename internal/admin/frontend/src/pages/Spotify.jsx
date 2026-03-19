import { useState } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Spotify() {
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
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
        toast('Authorization page opened. Complete the flow there.', 'success');
      } else {
        toast('Failed to get auth URL', 'error');
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
          <li>Add redirect URI: <code>http://YOUR_IP:PORT/api/spotify/exchange</code></li>
          <li>Enter credentials below and click Authorize</li>
        </ol>
      </div>

      <div class="card">
        <div class="card-title">Credentials</div>
        <div class="form-group">
          <label>Client ID</label>
          <input type="text" value={clientId} onInput={e => setClientId(e.target.value)} placeholder="Enter Spotify Client ID" />
        </div>
        <div class="form-group">
          <label>Client Secret</label>
          <input type="password" value={clientSecret} onInput={e => setClientSecret(e.target.value)} placeholder="Enter Spotify Client Secret" />
        </div>
        <button class="btn btn-primary" onClick={startAuth}>🔗 Authorize with Spotify</button>
      </div>
    </div>
  );
}
