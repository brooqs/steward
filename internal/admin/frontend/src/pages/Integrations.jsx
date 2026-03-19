import { useState, useEffect } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Integrations() {
  const [integrations, setIntegrations] = useState([]);
  const [templates, setTemplates] = useState([]);
  const [loading, setLoading] = useState(true);
  const [editName, setEditName] = useState(null);
  const [editContent, setEditContent] = useState('');
  const toast = useToast();

  const load = () => {
    Promise.all([
      fetch('/api/integrations').then(r => r.json()).then(d => d.integrations || []),
      fetch('/api/integrations/templates').then(r => r.json()).then(d => d.templates || []).catch(() => []),
    ]).then(([intgs, tmpls]) => {
      setIntegrations(intgs);
      setTemplates(tmpls);
      setLoading(false);
    }).catch(() => setLoading(false));
  };

  useEffect(load, []);

  const openEditor = async (name, content) => {
    if (content) {
      setEditName(name);
      setEditContent(content);
      return;
    }
    // Fetch content from API for existing integrations
    try {
      const res = await fetch(`/api/integrations?name=${name}`);
      const data = await res.json();
      setEditName(name);
      setEditContent(data.raw || '');
    } catch {
      toast('Failed to load integration config', 'error');
    }
  };

  const saveIntegration = async () => {
    try {
      const res = await fetch('/api/integrations/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: editName, content: editContent }),
      });
      if (res.ok) {
        toast(`${editName} saved!`, 'success');
        setEditName(null);
        load();
      } else {
        toast('Failed to save', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
  };

  if (loading) return <div class="page-title">Loading...</div>;

  return (
    <div>
      <h2 class="page-title">🔌 Integrations</h2>

      {editName ? (
        <div class="card">
          <div class="card-title">Editing: {editName}</div>
          <div class="form-group">
            <textarea rows="15" value={editContent} onInput={e => setEditContent(e.target.value)} style="font-family: monospace; font-size: 12px;" />
          </div>
          <div style="display: flex; gap: 8px;">
            <button class="btn btn-primary" onClick={saveIntegration}>💾 Save</button>
            <button class="btn btn-outline" onClick={() => setEditName(null)}>Cancel</button>
          </div>
        </div>
      ) : (
        <>
          <div class="card">
            <div class="card-title">Active Integrations</div>
            {integrations.length === 0 ? (
              <p style="color: var(--text-muted); font-size: 13px;">No integrations configured yet.</p>
            ) : (
              <table>
                <thead>
                  <tr><th>Name</th><th>Status</th><th>Action</th></tr>
                </thead>
                <tbody>
                  {integrations.map(intg => (
                    <tr key={intg.name}>
                      <td>{intg.name}</td>
                      <td><span class={`badge ${intg.enabled ? 'badge-success' : 'badge-warning'}`}>{intg.enabled ? 'Active' : 'Disabled'}</span></td>
                      <td><button class="btn btn-outline" onClick={() => openEditor(intg.name, intg.content)}>Edit</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          {templates.length > 0 && (
            <div class="card">
              <div class="card-title">Available Templates</div>
              <div style="display: flex; gap: 8px; flex-wrap: wrap;">
                {templates.map(t => (
                  <button key={t.name} class="btn btn-outline" onClick={() => openEditor(t.name, t.content)}>
                    + {t.name}
                  </button>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
