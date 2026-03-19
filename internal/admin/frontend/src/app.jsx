import Router from 'preact-router';
import { useState, useEffect } from 'preact/hooks';
import { Sidebar } from './components/Sidebar';
import { ToastProvider } from './components/Toast';
import { Dashboard } from './pages/Dashboard';
import { Settings } from './pages/Settings';
import { Integrations } from './pages/Integrations';
import { Spotify } from './pages/Spotify';
import { Google } from './pages/Google';
import { Policies } from './pages/Policies';
import { Channels } from './pages/Channels';
import { Cron } from './pages/Cron';
import { Logs } from './pages/Logs';
import { Setup } from './pages/Setup';

function RestartButton() {
  const [restarting, setRestarting] = useState(false);

  const handleRestart = async () => {
    if (!confirm('Restart Steward? The service will be briefly unavailable.')) return;
    setRestarting(true);
    try {
      await fetch('/api/restart', { method: 'POST' });
    } catch {}
    const poll = setInterval(async () => {
      try {
        const res = await fetch('/api/status');
        if (res.ok) {
          clearInterval(poll);
          window.location.reload();
        }
      } catch {}
    }, 2000);
  };

  if (restarting) {
    return (
      <div style="position: fixed; inset: 0; background: rgba(0,0,0,0.7); display: flex; align-items: center; justify-content: center; z-index: 9999;">
        <div style="text-align: center; color: white;">
          <div style="font-size: 32px; margin-bottom: 16px;">⏳</div>
          <div style="font-size: 18px; font-weight: 600;">Restarting Steward...</div>
          <div style="font-size: 13px; color: var(--text-muted); margin-top: 8px;">Page will reload automatically</div>
        </div>
      </div>
    );
  }

  return (
    <button
      onClick={handleRestart}
      title="Restart Steward"
      style="position: fixed; top: 16px; right: 24px; z-index: 100; background: var(--surface); border: 1px solid var(--border); color: var(--text-muted); padding: 8px 14px; border-radius: var(--radius-sm); cursor: pointer; font-size: 13px; display: flex; align-items: center; gap: 6px; transition: all 0.2s;"
      onMouseEnter={e => { e.target.style.borderColor = 'var(--warning)'; e.target.style.color = 'var(--warning)'; }}
      onMouseLeave={e => { e.target.style.borderColor = 'var(--border)'; e.target.style.color = 'var(--text-muted)'; }}
    >
      🔄 Restart
    </button>
  );
}

export function App() {
  const [setupMode, setSetupMode] = useState(null); // null = loading, true/false

  useEffect(() => {
    fetch('/api/status')
      .then(r => r.json())
      .then(data => setSetupMode(data.setup_mode === true))
      .catch(() => setSetupMode(false));
  }, []);

  // Loading
  if (setupMode === null) return null;

  // Setup mode — show wizard only, no sidebar
  if (setupMode) {
    return (
      <ToastProvider>
        <Setup />
      </ToastProvider>
    );
  }

  // Normal mode
  return (
    <ToastProvider>
      <div class="layout">
        <Sidebar />
        <main class="main-content">
          <RestartButton />
          <Router>
            <Dashboard path="/" />
            <Settings path="/settings" />
            <Channels path="/channels" />
            <Integrations path="/integrations" />
            <Spotify path="/spotify" />
            <Google path="/google" />
            <Policies path="/policies" />
            <Cron path="/cron" />
            <Logs path="/logs" />
          </Router>
        </main>
      </div>
    </ToastProvider>
  );
}
