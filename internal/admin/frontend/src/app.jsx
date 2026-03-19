import Router from 'preact-router';
import { Sidebar } from './components/Sidebar';
import { ToastProvider } from './components/Toast';
import { Dashboard } from './pages/Dashboard';
import { Settings } from './pages/Settings';
import { Integrations } from './pages/Integrations';
import { Spotify } from './pages/Spotify';
import { Google } from './pages/Google';
import { Policies } from './pages/Policies';
import { Cron } from './pages/Cron';
import { Logs } from './pages/Logs';

export function App() {
  return (
    <ToastProvider>
      <div class="layout">
        <Sidebar />
        <main class="main-content">
          <Router>
            <Dashboard path="/" />
            <Settings path="/settings" />
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
