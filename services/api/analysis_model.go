package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// The optional adapter accepts a minimal evidence envelope and returns a
// JSON analysis object. No raw event payload, identity or credential is sent.
func modelAnalysis(ctx context.Context, ruleID string, evidence []string) (incidentAnalysis, error) {
	return requestModelAnalysis(ctx, os.Getenv("SOC_ANALYST_URL"), os.Getenv("SOC_ANALYST_TOKEN"), ruleID, evidence, nil)
}

func requestModelAnalysis(ctx context.Context, endpoint, token, ruleID string, evidence []string, transport http.RoundTripper) (incidentAnalysis, error) {
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return incidentAnalysis{}, errors.New("HTTPS analysis adapter unavailable")
	}
	body, err := json.Marshal(struct {
		RuleID string   `json:"rule_id"`
		IDs    []string `json:"event_ids"`
	}{ruleID, evidence})
	if err != nil {
		return incidentAnalysis{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return incidentAnalysis{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(req)
	if err != nil {
		return incidentAnalysis{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return incidentAnalysis{}, errors.New("analysis adapter returned an unsuccessful status")
	}
	const maxResponse = 16 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return incidentAnalysis{}, errors.New("invalid analysis response size")
	}
	return validateAnalysis(data, evidence)
}

func validateAnalysis(data []byte, evidence []string) (incidentAnalysis, error) {
	var result incidentAnalysis
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&result) != nil || d.Decode(&extra) != io.EOF {
		return incidentAnalysis{}, errors.New("invalid analysis JSON")
	}
	if result.Mode != "" || len(strings.TrimSpace(result.Summary)) == 0 || len(result.Summary) > 2000 ||
		result.ReferencedEventIDs == nil || result.ProposedNextSteps == nil || len(result.ProposedNextSteps) > 5 {
		return incidentAnalysis{}, errors.New("invalid analysis fields")
	}
	switch result.Confidence {
	case "low", "medium", "high":
	default:
		return incidentAnalysis{}, errors.New("invalid analysis confidence")
	}
	known := make(map[string]bool, len(evidence))
	for _, id := range evidence {
		known[id] = true
	}
	if len(evidence) > 0 && len(result.ReferencedEventIDs) == 0 {
		return incidentAnalysis{}, errors.New("analysis omitted evidence")
	}
	for _, id := range result.ReferencedEventIDs {
		if !known[id] {
			return incidentAnalysis{}, errors.New("unknown or duplicate evidence ID")
		}
		delete(known, id)
	}
	for _, step := range result.ProposedNextSteps {
		if len(strings.TrimSpace(step)) == 0 || len(step) > 400 {
			return incidentAnalysis{}, errors.New("invalid analysis next step")
		}
	}
	result.Mode = "adapter"
	return result, nil
}
