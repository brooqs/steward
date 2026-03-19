import { useState, useEffect } from 'preact/hooks';
import { getCurrentUrl } from 'preact-router';

const navItems = [
  { section: 'Overview' },
  { path: '/', icon: '📊', label: 'Dashboard' },
  { section: 'Configuration' },
  { path: '/settings', icon: '⚙️', label: 'Settings' },
  { path: '/integrations', icon: '🔌', label: 'Integrations' },
  { path: '/policies', icon: '🛡️', label: 'Policies' },
  { section: 'Services' },
  { path: '/spotify', icon: '🎵', label: 'Spotify' },
  { path: '/google', icon: '📧', label: 'Google' },
  { section: 'System' },
  { path: '/cron', icon: '⏰', label: 'Cron Jobs' },
  { path: '/logs', icon: '📋', label: 'Logs' },
];

export function Sidebar() {
  const [url, setUrl] = useState(getCurrentUrl());

  useEffect(() => {
    const onRouteChange = () => setUrl(getCurrentUrl());
    addEventListener('popstate', onRouteChange);
    return () => removeEventListener('popstate', onRouteChange);
  }, []);

  const handleClick = (path, e) => {
    e.preventDefault();
    history.pushState(null, '', path);
    setUrl(path);
    dispatchEvent(new PopStateEvent('popstate'));
  };

  return (
    <nav class="sidebar">
      <div class="sidebar-brand">
        <h1>🤖 Steward</h1>
        <span>AI Assistant</span>
      </div>
      {navItems.map((item, i) => {
        if (item.section) {
          return <div key={`s-${i}`} class="sidebar-section">{item.section}</div>;
        }
        const isActive = item.path === '/' ? url === '/' : url.startsWith(item.path);
        return (
          <a
            key={item.path}
            href={item.path}
            class={`nav-item ${isActive ? 'active' : ''}`}
            onClick={(e) => handleClick(item.path, e)}
          >
            <span class="icon">{item.icon}</span>
            <span>{item.label}</span>
          </a>
        );
      })}
    </nav>
  );
}
