package devflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	julesSessionsURL      = "https://jules.googleapis.com/v1alpha/sessions/"
	julesActivitiesSuffix = "/activities?pageSize=100"
	julesSendMessageVerb  = ":sendMessage"
	julesApprovePlanVerb  = ":approvePlan"
)

func julesSessionURL(sessionID string) string { return julesSessionsURL + sessionID }

// JulesSendMessage answers a Jules session: the text reaches the agent as a
// user message and resumes it (a question, a correction, "open the PR").
func JulesSendMessage(sessionID, apiKey, text string, client HTTPClient) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty reply: write what Jules should do")
	}
	body, err := json.Marshal(struct {
		Prompt string `json:"prompt"`
	}{text})
	if err != nil {
		return err
	}
	_, err = julesCall(http.MethodPost, julesSessionURL(sessionID)+julesSendMessageVerb, apiKey, body, client)
	return err
}

// JulesApprovePlan approves the plan a session is waiting on
// (state AWAITING_PLAN_APPROVAL).
func JulesApprovePlan(sessionID, apiKey string, client HTTPClient) error {
	_, err := julesCall(http.MethodPost, julesSessionURL(sessionID)+julesApprovePlanVerb, apiKey, []byte("{}"), client)
	return err
}

// julesLastAgentMessage returns the agent's most recent message in the
// session, "" when it has none. The API lists activities oldest first.
func julesLastAgentMessage(sessionID, apiKey string, client HTTPClient) (string, error) {
	url := julesSessionURL(sessionID) + julesActivitiesSuffix
	last := ""
	for pageToken := ""; ; {
		pageURL := url
		if pageToken != "" {
			pageURL += "&pageToken=" + pageToken
		}
		raw, err := julesCall(http.MethodGet, pageURL, apiKey, nil, client)
		if err != nil {
			return "", err
		}
		var page struct {
			Activities []struct {
				AgentMessaged *struct {
					AgentMessage string `json:"agentMessage"`
				} `json:"agentMessaged"`
			} `json:"activities"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", fmt.Errorf("could not decode Jules activities: %w", err)
		}
		for _, a := range page.Activities {
			if a.AgentMessaged != nil && a.AgentMessaged.AgentMessage != "" {
				last = a.AgentMessaged.AgentMessage
			}
		}
		if page.NextPageToken == "" {
			return last, nil
		}
		pageToken = page.NextPageToken
	}
}

// julesCall sends one authenticated request to the Jules API and returns the
// body of a 200 answer.
func julesCall(method, url, apiKey string, body []byte, client HTTPClient) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}
	req.Header.Set("X-Goog-Api-Key", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jules API request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jules API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}
