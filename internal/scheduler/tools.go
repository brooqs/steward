package scheduler

import (
	"fmt"

	"github.com/brooqs/steward/internal/tools"
)

// GetTools returns the cron management tools for the AI.
func (s *Scheduler) GetTools() []tools.ToolSpec {
	return []tools.ToolSpec{
		{
			Name:        "cron_create",
			Description: "Create a scheduled cron job. The job will fire at the specified schedule and send the prompt to the AI for processing. The AI response will be sent to the specified channel.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string", "description": "Human-readable job name (e.g. 'Morning Briefing')"},
					"schedule": map[string]any{"type": "string", "description": "Cron expression (5-field: minute hour day month weekday). Examples: '0 8 * * *' = daily 8AM, '0 9 * * 1-5' = weekdays 9AM, '*/30 * * * *' = every 30 min"},
					"prompt":   map[string]any{"type": "string", "description": "The prompt to send to the AI when the job fires"},
					"channel":  map[string]any{"type": "string", "description": "Notification channel (e.g. 'whatsapp:905xxxxxxxxxx')"},
				},
				"required": []string{"name", "schedule", "prompt", "channel"},
			},
			Handler: s.toolCreate,
		},
		{
			Name:        "cron_list",
			Description: "List all scheduled cron jobs",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.toolList,
		},
		{
			Name:        "cron_delete",
			Description: "Delete a scheduled cron job",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"job_id": map[string]any{"type": "string", "description": "Job ID to delete (from cron_list)"},
				},
				"required": []string{"job_id"},
			},
			Handler: s.toolDelete,
		},
	}
}

func (s *Scheduler) toolCreate(params map[string]any) (any, error) {
	name, _ := params["name"].(string)
	schedule, _ := params["schedule"].(string)
	prompt, _ := params["prompt"].(string)
	channel, _ := params["channel"].(string)

	if name == "" || schedule == "" || prompt == "" || channel == "" {
		return nil, fmt.Errorf("name, schedule, prompt, and channel are all required")
	}

	job, err := s.AddJob(name, schedule, prompt, channel)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return map[string]any{
		"status":   "created",
		"id":       job.ID,
		"name":     job.Name,
		"schedule": job.Schedule,
		"prompt":   job.Prompt,
		"channel":  job.Channel,
	}, nil
}

func (s *Scheduler) toolList(params map[string]any) (any, error) {
	jobs := s.ListJobs()
	if len(jobs) == 0 {
		return map[string]any{"jobs": []any{}, "count": 0}, nil
	}

	var result []map[string]any
	for _, j := range jobs {
		result = append(result, map[string]any{
			"id":         j.ID,
			"name":       j.Name,
			"schedule":   j.Schedule,
			"prompt":     j.Prompt,
			"channel":    j.Channel,
			"enabled":    j.Enabled,
			"created_at": j.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return map[string]any{"jobs": result, "count": len(result)}, nil
}

func (s *Scheduler) toolDelete(params map[string]any) (any, error) {
	jobID, _ := params["job_id"].(string)
	if jobID == "" {
		return nil, fmt.Errorf("job_id required")
	}

	if err := s.RemoveJob(jobID); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return map[string]any{"status": "deleted", "job_id": jobID}, nil
}
