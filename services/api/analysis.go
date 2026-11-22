package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type incidentAnalysis struct {
	Mode               string   `json:"mode"`
	Summary            string   `json:"summary"`
	Confidence         string   `json:"confidence"`
	ReferencedEventIDs []string `json:"referenced_event_ids"`
	ProposedNextSteps  []string `json:"proposed_next_steps"`
}

func offlineAnalysis(ruleID string, evidence []string) incidentAnalysis {
	analysis := incidentAnalysis{
		Mode:               "offline",
		ReferencedEventIDs: append([]string{}, evidence...),
		Confidence:         "low",
		ProposedNextSteps:  []string{"Review the alert and agent heartbeat history."},
		Summary:            fmt.Sprintf("Rule %s created an incident with no linked source events.", ruleID),
	}
	if len(evidence) > 0 {
		analysis.Confidence = "medium"
		analysis.Summary = fmt.Sprintf("Rule %s created an incident. This explanation cites %d linked source events.", ruleID, len(evidence))
		analysis.ProposedNextSteps = []string{"Review the cited events and verify their timing and source with the lab operator."}
	}
	return analysis
}

func (s *Server) handleAnalyse(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}
	var job string
	err := s.pool.QueryRow(r.Context(), `
		INSERT INTO analysis_jobs (tenant_id, incident_id, requested_by)
		SELECT tenant_id, id, $3 FROM incidents WHERE id = $1::uuid AND tenant_id = $2::uuid
		ON CONFLICT (tenant_id, incident_id) WHERE status IN ('queued','running')
		DO UPDATE SET incident_id = EXCLUDED.incident_id
		RETURNING id::text`, id, sess.TenantID, sess.Subject).Scan(&job)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not request incident analysis"})
	default:
		s.audit(r.Context(), sess.TenantID, sess.Subject, "incident.analyse.request", id, "accepted")
		writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job})
	}
}

func (s *Server) handleAnalysisResult(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id, job := r.PathValue("id"), r.PathValue("job")
	if !uuidRe.MatchString(id) || !uuidRe.MatchString(job) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "analysis job not found"})
		return
	}
	var status string
	var result json.RawMessage
	err := s.pool.QueryRow(r.Context(), `
		SELECT status, COALESCE(result,'null'::jsonb) FROM analysis_jobs
		WHERE id = $1::uuid AND incident_id = $2::uuid AND tenant_id = $3::uuid`,
		job, id, sess.TenantID).Scan(&status, &result)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "analysis job not found"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load incident analysis"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "result": result})
	}
}

func (s *Server) runAnalysisJobs(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.processAnalysisJob(ctx); err != nil {
				log.Printf("incident analysis worker: %v", err)
				// Back off on a database or migration fault.
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}
	}
}

func (s *Server) processAnalysisJob(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var job, incident, tenant, actor string
	var attempt int
	err := s.pool.QueryRow(ctx, `
		UPDATE analysis_jobs SET status = 'running', claimed_at = now(), attempts = attempts + 1
		WHERE id = (
			SELECT id FROM analysis_jobs
			WHERE status = 'queued' OR (status = 'running' AND claimed_at < now() - interval '30 seconds')
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING id::text, incident_id::text, tenant_id::text, requested_by, attempts`).
		Scan(&job, &incident, &tenant, &actor, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	var ruleID string
	var evidence []string
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(i.rule_id, 'unassigned'), ARRAY(
			SELECT e.id::text FROM incident_events ie JOIN events e ON e.id = ie.event_id
			WHERE ie.incident_id = i.id AND e.tenant_id = i.tenant_id
			ORDER BY e.occurred_at, e.id LIMIT 100
		)
		FROM incidents i WHERE i.id = $1::uuid AND i.tenant_id = $2::uuid`, incident, tenant).
		Scan(&ruleID, &evidence)
	status, outcome := "failed", "failed"
	var data []byte
	if err == nil {
		analysis, err := modelAnalysis(ctx, ruleID, evidence)
		if err != nil {
			analysis = offlineAnalysis(ruleID, evidence)
		}
		data, err = json.Marshal(analysis)
		if err == nil {
			status, outcome = "completed", analysis.Mode
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE analysis_jobs SET status = $2, result = NULLIF($3,'')::jsonb
		WHERE id = $1::uuid AND attempts = $4 AND status = 'running'`, job, status, string(data), attempt)
	if err == nil && tag.RowsAffected() == 1 {
		s.audit(ctx, tenant, actor, "incident.analyse", incident, outcome)
	}
	return err
}
