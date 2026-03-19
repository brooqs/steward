import { useState } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Google() {
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const toast = useToast();

  const startAuth = async () => {
    if (!clientId || !clientSecret) {
      toast('Client ID and Secret are required', 'error');
      return;
    }
    try {
      const res = await fetch('/api/gmail/authorize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ client_id: clientId, client_secret: clientSecret }),
      });
      const data = await res.json();
      if (data.auth_url) {
        window.open(data.auth_url, '_blank');
        toast('Google authorization page opened. Complete the flow there.', 'success');
      } else {
        toast('Failed to get auth URL', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
  };

  return (
    <div>
      <h2 class="page-title">📧 Google Setup</h2>

      <div class="card">
        <div class="card-title">OAuth2 Configuration</div>
        <p style="font-size: 13px; color: var(--text-secondary); margin-bottom: 12px;">
          Connect your Google account for Gmail, Calendar, and Drive access.
        </p>
        <ol class="step-list">
          <li>Go to <a href="https://console.cloud.google.com/apis/credentials" target="_blank">Google Cloud Console</a></li>
          <li>Create an OAuth2 client (Web application)</li>
          <li>Enable Gmail API, Calendar API, and Drive API</li>
          <li>Add redirect URI: <code>http://YOUR_IP:PORT/api/gmail/exchange</code></li>
          <li>Enter credentials below and click Authorize</li>
        </ol>
      </div>

      <div class="card">
        <div class="card-title">Credentials</div>
        <div class="form-group">
          <label>Client ID</label>
          <input type="text" value={clientId} onInput={e => setClientId(e.target.value)} placeholder="xxxx.apps.googleusercontent.com" />
        </div>
        <div class="form-group">
          <label>Client Secret</label>
          <input type="password" value={clientSecret} onInput={e => setClientSecret(e.target.value)} placeholder="GOCSPX-..." />
        </div>
        <button class="btn btn-primary" onClick={startAuth}>🔗 Authorize with Google</button>
      </div>

      <div class="card">
        <div class="card-title">Services Included</div>
        <div style="display: flex; gap: 8px; flex-wrap: wrap;">
          <span class="badge badge-success">📧 Gmail (6 tools)</span>
          <span class="badge badge-success">📅 Calendar (5 tools)</span>
          <span class="badge badge-success">📁 Drive (5 tools)</span>
        </div>
      </div>
    </div>
  );
}
