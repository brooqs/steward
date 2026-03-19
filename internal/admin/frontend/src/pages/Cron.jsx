import { useState, useEffect } from 'preact/hooks';
import { useToast } from '../components/Toast';

export function Cron() {
  const [jobs, setJobs] = useState([]);
  const [loading, setLoading] = useState(true);
  const toast = useToast();

  const load = () => {
    fetch('/api/cron/jobs')
      .then(r => r.json())
      .then(data => {
        setJobs(data.jobs || []);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  };

  useEffect(load, []);

  const deleteJob = async (jobId, jobName) => {
    if (!confirm(`Delete cron job "${jobName}"?`)) return;
    try {
      const res = await fetch('/api/cron/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ job_id: jobId }),
      });
      if (res.ok) {
        toast(`Job "${jobName}" deleted`, 'success');
        load();
      } else {
        toast('Failed to delete job', 'error');
      }
    } catch {
      toast('Network error', 'error');
    }
  };

  if (loading) return <div class="page-title">Loading...</div>;

  return (
    <div>
      <h2 class="page-title">⏰ Cron Jobs</h2>

      <div class="card">
        <div class="card-title">Scheduled Tasks</div>
        <p style="font-size: 12px; color: var(--text-muted); margin-bottom: 16px;">
          Cron jobs are managed by the AI via the <code>cron_create</code> tool. Ask your AI assistant to create scheduled tasks.
        </p>

        {jobs.length === 0 ? (
          <p style="color: var(--text-muted); font-size: 13px;">
            No active cron jobs. Ask the AI: "Her sabah 8'de takvimimi özetle"
          </p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Schedule</th>
                <th>Prompt</th>
                <th>Channel</th>
                <th>Status</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map(job => (
                <tr key={job.id}>
                  <td style="font-weight: 600;">{job.name}</td>
                  <td><code style="font-size: 12px; background: var(--bg-hover); padding: 2px 6px; border-radius: 3px;">{job.schedule}</code></td>
                  <td style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{job.prompt}</td>
                  <td><span class="badge badge-success">{job.channel}</span></td>
                  <td><span class={`badge ${job.enabled ? 'badge-success' : 'badge-warning'}`}>{job.enabled ? 'Active' : 'Paused'}</span></td>
                  <td><button class="btn btn-danger" style="padding: 4px 10px; font-size: 11px;" onClick={() => deleteJob(job.id, job.name)}>Delete</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div class="card">
        <div class="card-title">Cron Expression Examples</div>
        <table>
          <tbody>
            <tr><td><code>0 8 * * *</code></td><td>Her gün 08:00</td></tr>
            <tr><td><code>0 9 * * 1-5</code></td><td>Hafta içi 09:00</td></tr>
            <tr><td><code>*/30 * * * *</code></td><td>Her 30 dakika</td></tr>
            <tr><td><code>0 23 * * 0</code></td><td>Her Pazar 23:00</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}
