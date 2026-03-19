import { useState, useEffect, useRef } from 'preact/hooks';

export function Logs() {
  const [logs, setLogs] = useState([]);
  const [filter, setFilter] = useState('all');
  const [paused, setPaused] = useState(false);
  const logEndRef = useRef(null);
  const pausedRef = useRef(false);

  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    let interval;
    let lastLen = 0;

    const fetchLogs = async () => {
      try {
        const res = await fetch('/api/logs');
        const data = await res.json();
        if (data.lines && data.lines.length !== lastLen) {
          lastLen = data.lines.length;
          if (!pausedRef.current) {
            setLogs(data.lines);
          }
        }
      } catch { /* ignore */ }
    };

    fetchLogs();
    interval = setInterval(fetchLogs, 2000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (!paused && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, paused]);

  const getLogClass = (line) => {
    if (line.includes('level=ERROR')) return 'log-error';
    if (line.includes('level=WARN')) return 'log-warn';
    if (line.includes('level=INFO')) return 'log-info';
    return '';
  };

  const filteredLogs = filter === 'all'
    ? logs
    : logs.filter(l => l.includes(`level=${filter.toUpperCase()}`));

  return (
    <div>
      <h2 class="page-title">📋 Live Logs</h2>

      <div style="display: flex; gap: 8px; margin-bottom: 16px; align-items: center;">
        <select value={filter} onChange={e => setFilter(e.target.value)} style="width: auto;">
          <option value="all">All Levels</option>
          <option value="info">INFO</option>
          <option value="warn">WARN</option>
          <option value="error">ERROR</option>
        </select>
        <button class={`btn ${paused ? 'btn-primary' : 'btn-outline'}`} onClick={() => setPaused(!paused)}>
          {paused ? '▶️ Resume' : '⏸️ Pause'}
        </button>
        <span style="font-size: 12px; color: var(--text-muted);">
          {filteredLogs.length} lines {paused ? '(paused)' : ''}
        </span>
      </div>

      <div class="log-viewer">
        {filteredLogs.length === 0 ? (
          <div style="color: var(--text-muted);">No logs yet...</div>
        ) : (
          filteredLogs.map((line, i) => (
            <div key={i} class={`log-line ${getLogClass(line)}`}>{line}</div>
          ))
        )}
        <div ref={logEndRef} />
      </div>
    </div>
  );
}
