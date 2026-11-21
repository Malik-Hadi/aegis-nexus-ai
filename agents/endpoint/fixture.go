package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// loadFixture reads simulations/*.json: {"events":[{kind, occurred_at, severity, payload, idempotency_key}, ...]}
func loadFixture(path string) ([]outEvent, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Events []outEvent `json:"events"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("bad fixture file: %w", err)
	}
	if len(f.Events) == 0 {
		return nil, errors.New("fixture has no events")
	}
	return f.Events, nil
}

// prepare moves the fixture to "now" (keeping the spacing between events, so rules like
// "6 failures in 5 minutes" still behave the same) and makes idempotency keys unique per run.
func prepare(in []outEvent, now time.Time, rebase, unique bool, runID string) ([]outEvent, error) {
	out := make([]outEvent, len(in))
	copy(out, in)
	if rebase {
		times := make([]time.Time, len(out))
		var latest time.Time
		for i, ev := range out {
			t, err := time.Parse(time.RFC3339, ev.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("event %d: bad occurred_at %q", i+1, ev.OccurredAt)
			}
			times[i] = t
			if t.After(latest) {
				latest = t
			}
		}
		delta := now.Sub(latest)
		for i := range out {
			out[i].OccurredAt = times[i].Add(delta).UTC().Format(time.RFC3339)
		}
	}
	if unique {
		for i := range out {
			k := out[i].IdempotencyKey + "-" + runID
			if len(k) > 128 {
				k = k[len(k)-128:]
			}
			out[i].IdempotencyKey = k
		}
	}
	return out, nil
}
