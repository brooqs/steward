import { useState, useEffect } from 'preact/hooks';
import { useToast } from '../components/Toast';

const DEFAULT_POLICIES = [
  'Kullanıcının açık onayı olmadan email gönderme',
  'Shell komutlarında rm -rf, format, shutdown, reboot gibi yıkıcı komutlar çalıştırma',
  'Kişisel bilgileri (şifre, token, API key) asla response içinde gösterme',
  'HomeAssistant ile kapı kilidi ve alarm sistemini onaysız kontrol etme',
  'Toplu email silme veya taşıma işlemi yapma',
  'Cron job ile tanımadığın kişilere mesaj gönderme',
  'Web browsing ile kişisel bilgileri paylaşma',
  'Para transferi veya alışveriş işlemi yapma',
];

export function Policies() {
  const [policies, setPolicies] = useState([]);
  const [newPolicy, setNewPolicy] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  useEffect(() => {
    fetch('/api/policies')
      .then(r => r.json())
      .then(data => {
        setPolicies(data.policies || []);
        setLoading(false);
      })
      .catch(() => {
        setPolicies([...DEFAULT_POLICIES]);
        setLoading(false);
      });
  }, []);

  const addPolicy = () => {
    if (!newPolicy.trim()) return;
    setPolicies(prev => [...prev, newPolicy.trim()]);
    setNewPolicy('');
  };

  const removePolicy = (index) => {
    setPolicies(prev => prev.filter((_, i) => i !== index));
  };

  const loadDefaults = () => {
    setPolicies([...DEFAULT_POLICIES]);
    toast('Default policies loaded', 'success');
  };

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/policies/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ policies }),
      });
      if (res.ok) {
        toast('Policies saved! They will take effect on next AI interaction.', 'success');
      } else {
        toast('Failed to save policies', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
    setSaving(false);
  };

  if (loading) return <div class="page-title">Loading...</div>;

  return (
    <div>
      <h2 class="page-title">🛡️ AI Policies</h2>

      <div class="card">
        <div class="card-title">Policy Rules</div>
        <p style="font-size: 12px; color: var(--text-muted); margin-bottom: 16px;">
          These rules are injected into the AI's system prompt. The AI is instructed to NEVER violate these policies.
        </p>

        {policies.length === 0 ? (
          <p style="color: var(--text-muted); font-size: 13px; padding: 12px 0;">
            No policies defined. Click "Load Defaults" to add recommended policies.
          </p>
        ) : (
          <div style="border: 1px solid var(--border); border-radius: var(--radius-sm);">
            {policies.map((policy, index) => (
              <div key={index} class="policy-item">
                <span style="color: var(--error); font-size: 16px;">🚫</span>
                <span class="policy-text">{policy}</span>
                <button class="btn btn-outline" style="padding: 4px 10px; font-size: 11px;" onClick={() => removePolicy(index)}>✕</button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div class="card">
        <div class="card-title">Add Custom Policy</div>
        <div style="display: flex; gap: 8px;">
          <input type="text" value={newPolicy} onInput={e => setNewPolicy(e.target.value)} placeholder="AI should never..." style="flex: 1;" onKeyDown={e => e.key === 'Enter' && addPolicy()} />
          <button class="btn btn-outline" onClick={addPolicy}>+ Add</button>
        </div>
      </div>

      <div style="display: flex; gap: 8px;">
        <button class="btn btn-primary" onClick={save} disabled={saving}>
          {saving ? '⏳ Saving...' : '💾 Save Policies'}
        </button>
        <button class="btn btn-outline" onClick={loadDefaults}>📋 Load Defaults</button>
      </div>
    </div>
  );
}
