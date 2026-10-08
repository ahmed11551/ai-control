package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Run with AI_CONTROL_E2E_TOKEN against a running local stack.
func TestControlLifecycleE2E(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("AI_CONTROL_E2E_TOKEN"))
	if token == "" {
	t.Skip("set AI_CONTROL_E2E_TOKEN to run against a live AI Control")
	}
	baseURL := strings.TrimRight(os.Getenv("AI_CONTROL_E2E_URL"), "/")
	if baseURL == "" {
	baseURL = "http://127.0.0.1:8091"
	}
	aggregateID := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 3 * time.Second}

	first := crmEventForE2E(aggregateID, 1, "new", "")
	postE2EEvent(t, client, baseURL, token, first, http.StatusAccepted)
	waitForFindingState(t, client, baseURL, token, aggregateID, "open")
	postE2EEvent(t, client, baseURL, token, first, http.StatusOK)

	second := crmEventForE2E(aggregateID, 2, "closed", "")
	postE2EEvent(t, client, baseURL, token, second, http.StatusAccepted)
	waitForFindingState(t, client, baseURL, token, aggregateID, "resolved")
}

func crmEventForE2E(aggregateID string, version int, stage, nextAction string) CRMEvent {
	eventID := fmt.Sprintf("%s-%d", aggregateID, version)
	return CRMEvent{
	EventID: eventID, EventType: "lead.updated", SchemaVersion: "crm-event-v1",
	OccurredAt: time.Now().UTC(), Producer: "go-e2e-test", CorrelationID: eventID,
	TraceID: eventID, AggregateType: "lead", AggregateID: aggregateID,
	AggregateVersion: version, DataClass: "C1",
	Payload: json.RawMessage(fmt.Sprintf(`{"stage":%q,"next_action":%q}`, stage, nextAction)),
	}
}

func postE2EEvent(t *testing.T, client *http.Client, baseURL, token string, event CRMEvent, wantStatus int) {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
	t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/events", bytes.NewReader(body))
	if err != nil {
	t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Token", token)
	res, err := client.Do(req)
	if err != nil {
	t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
	t.Fatalf("event %s: got HTTP %d, want %d", event.EventID, res.StatusCode, wantStatus)
	}
}

func waitForFindingState(t *testing.T, client *http.Client, baseURL, token, aggregateID, wantState string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/findings?aggregate_type=lead", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-AI-Token", token)
	res, err := client.Do(req)
	if err == nil {
		var findings []struct {
			State  string          `json:"state"`
			Result json.RawMessage `json:"result"`
		}
		_ = json.NewDecoder(res.Body).Decode(&findings)
		res.Body.Close()
		for _, finding := range findings {
			var result struct {
				AggregateID string `json:"aggregate_id"`
			}
			if json.Unmarshal(finding.Result, &result) == nil && result.AggregateID == aggregateID && finding.State == wantState {
				return
			}
		}
	}
	time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("finding for aggregate %s did not reach state %q", aggregateID, wantState)
}
