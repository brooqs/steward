import { useState, useEffect } from 'preact/hooks';

export function Dashboard() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/status')
      .then(r => r.json())
      .then(data => { setStatus(data); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  if (loading) return <div class="page-title">Loading...</div>;
  if (!status) return <div class="page-title">Failed to load status</div>;

  return (
    <div>
      <h2 class="page-title">📊 Dashboard</h2>

      <div class="stat-grid">
        <div class="stat-card">
          <div class="value">{status.tool_count || 0}</div>
          <div class="label">Active Tools</div>
        </div>
        <div class="stat-card">
          <div class="value">{(status.integrations || []).length}</div>
          <div class="label">Integrations</div>
        </div>
        <div class="stat-card">
          <div class="value">{status.uptime_human || '—'}</div>
          <div class="label">Uptime</div>
        </div>
        <div class="stat-card">
          <div class="value">{status.version || 'dev'}</div>
          <div class="label">Version</div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">System Information</div>
        <table>
          <tbody>
            <tr><td>Provider</td><td><span class="badge badge-success">{status.provider}</span></td></tr>
            <tr><td>Model</td><td>{status.model}</td></tr>
            <tr><td>Memory Backend</td><td>{status.memory_backend}</td></tr>
            <tr><td>Channel</td><td>{status.channel}</td></tr>
            <tr><td>Voice STT</td><td>{status.voice_stt || 'disabled'}</td></tr>
            <tr><td>Voice TTS</td><td>{status.voice_tts || 'disabled'}</td></tr>
            <tr><td>Satellite</td><td>{status.satellite_enabled ? '✅ Enabled' : '❌ Disabled'}</td></tr>
          </tbody>
        </table>
      </div>

      {(status.integrations || []).length > 0 && (
        <div class="card">
          <div class="card-title">Active Integrations</div>
          <div style="display: flex; gap: 8px; flex-wrap: wrap;">
            {status.integrations.map(name => (
              <span key={name} class="badge badge-success">{name}</span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
