// Package scheduler provides an AI-managed cron scheduler.
// Jobs are created by the AI via tools and fire as prompts back to the AI.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// ChatFunc is called when a job fires. It sends a prompt to the AI and returns its response.
type ChatFunc func(ctx context.Context, sessionID, prompt string) (string, error)

// NotifyFunc sends a message to a channel (e.g. "whatsapp:905xxx").
type NotifyFunc func(channel, message string) error

// Job represents a scheduled task.
type Job struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`  // cron expression (5-field)
	Prompt    string    `json:"prompt"`    // prompt sent to AI when fired
	Channel   string    `json:"channel"`   // notification channel (e.g. "whatsapp:905xxx")
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// Scheduler manages cron jobs.
type Scheduler struct {
	jobs     map[string]*Job
	cron     *cron.Cron
	entryMap map[string]cron.EntryID // job ID → cron entry ID
	chatFn   ChatFunc
	notifyFn NotifyFunc
	savePath string
	mu       sync.RWMutex
}

// Config holds scheduler configuration.
type Config struct {
	SavePath string     // path to persist jobs (e.g. config/scheduler.json)
	ChatFn   ChatFunc   // function to send prompts to the AI
	NotifyFn NotifyFunc // function to send results to channels
}

// New creates a new Scheduler.
func New(cfg Config) *Scheduler {
	return &Scheduler{
		jobs:     make(map[string]*Job),
		cron:     cron.New(),
		entryMap: make(map[string]cron.EntryID),
		chatFn:   cfg.ChatFn,
		notifyFn: cfg.NotifyFn,
		savePath: cfg.SavePath,
	}
}

// Start loads persisted jobs and starts the cron scheduler.
func (s *Scheduler) Start() error {
	// Load persisted jobs
	if err := s.load(); err != nil {
		slog.Warn("scheduler: no saved jobs", "error", err)
	}

	// Schedule all loaded jobs
	s.mu.RLock()
	for _, job := range s.jobs {
		if job.Enabled {
			if err := s.scheduleJob(job); err != nil {
				slog.Error("scheduler: failed to schedule job", "id", job.ID, "name", job.Name, "error", err)
			}
		}
	}
	s.mu.RUnlock()

	s.cron.Start()
	slog.Info("scheduler started", "jobs", len(s.jobs))
	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	slog.Info("scheduler stopped")
}

// AddJob creates a new scheduled job.
func (s *Scheduler) AddJob(name, schedule, prompt, channel string) (*Job, error) {
	// Validate cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", schedule, err)
	}

	job := &Job{
		ID:        uuid.New().String()[:8],
		Name:      name,
		Schedule:  schedule,
		Prompt:    prompt,
		Channel:   channel,
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	if err := s.scheduleJob(job); err != nil {
		return nil, err
	}

	if err := s.save(); err != nil {
		slog.Warn("scheduler: failed to save jobs", "error", err)
	}

	slog.Info("scheduler: job created", "id", job.ID, "name", name, "schedule", schedule)
	return job, nil
}

// RemoveJob deletes a job by ID.
func (s *Scheduler) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[id]; !exists {
		return fmt.Errorf("job %q not found", id)
	}

	// Remove from cron
	if entryID, ok := s.entryMap[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entryMap, id)
	}

	delete(s.jobs, id)

	if err := s.save(); err != nil {
		slog.Warn("scheduler: failed to save jobs", "error", err)
	}

	slog.Info("scheduler: job deleted", "id", id)
	return nil
}

// ListJobs returns all jobs.
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

// scheduleJob adds a job to the cron scheduler.
func (s *Scheduler) scheduleJob(job *Job) error {
	jobCopy := *job // capture for closure
	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.fireJob(&jobCopy)
	})
	if err != nil {
		return fmt.Errorf("scheduling %q: %w", job.Name, err)
	}

	s.mu.Lock()
	s.entryMap[job.ID] = entryID
	s.mu.Unlock()
	return nil
}

// fireJob executes a scheduled job by sending its prompt to the AI.
func (s *Scheduler) fireJob(job *Job) {
	slog.Info("scheduler: firing job", "id", job.ID, "name", job.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sessionID := "cron:" + job.ID
	response, err := s.chatFn(ctx, sessionID, job.Prompt)
	if err != nil {
		slog.Error("scheduler: AI error", "job", job.Name, "error", err)
		response = fmt.Sprintf("⏰ Cron job '%s' failed: %s", job.Name, err.Error())
	}

	// Prepend job name for context
	fullResponse := fmt.Sprintf("⏰ *%s*\n\n%s", job.Name, response)

	if err := s.notifyFn(job.Channel, fullResponse); err != nil {
		slog.Error("scheduler: notify error", "job", job.Name, "channel", job.Channel, "error", err)
	}
}

// ── Persistence ──────────────────────────────────────────────

func (s *Scheduler) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.savePath, data, 0o644)
}

func (s *Scheduler) load() error {
	data, err := os.ReadFile(s.savePath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.jobs)
}
