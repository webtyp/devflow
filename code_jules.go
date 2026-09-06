package devflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"io"
	"net/http"
	"strings"
	"time"

	keyring "webtyp.com/keyring/auto"
)

// HTTPClient defines the interface for HTTP operations (injectable for tests).
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// JulesConfig holds the configuration for the Jules driver.
// All fields are optional: APIKey is loaded from keyring if empty,
// SourceID and StartBranch are auto-detected via gh/git if empty.
type JulesConfig struct {
	APIKey              string        // optional: loaded from keyring if empty
	SourceID            string        // optional: auto-detected via gh CLI if empty
	StartBranch         string        // optional: auto-detected via git if empty
	SessionTitle        string        // optional: defaults to prompt filename
	SourceIndexTimeout  time.Duration // optional: max wait for source to appear (default 2m)
	SourceIndexInterval time.Duration // optional: polling interval for source check (default 10s)
}

// JulesResultPrefix is the prefix on the string returned by the Jules driver
// after dispatching a session. cmd/codejob uses it to format the output.
const JulesResultPrefix = "jules: "

// JulesDriver implements CodeJobDriver for the Jules AI agent.
type JulesDriver struct {
	config    JulesConfig
	http      HTTPClient
	log       func(...any)
	sessionID string
	runner    gitmod.Runner
}

// NewJulesDriver creates a JulesDriver. All JulesConfig fields are optional.
func NewJulesDriver(config JulesConfig) *JulesDriver {
	return &JulesDriver{
		config: config,
		http:   &http.Client{},
		log:    func(...any) {},
		runner: gitmod.RealRunner{},
	}
}

// SetRunner replaces the command runner used for the gh session check (for testing).
func (d *JulesDriver) SetRunner(r gitmod.Runner) {
	if r != nil {
		d.runner = r
	}
}

// Name returns the driver name.
func (d *JulesDriver) Name() string { return "Jules" }

// SessionID returns the last session ID created.
func (d *JulesDriver) SessionID() string { return d.sessionID }

// SetLog sets the logging function.
func (d *JulesDriver) SetLog(fn func(...any)) {
	if fn != nil {
		d.log = fn
	}
}

// SetHTTPClient replaces the HTTP client (for testing).
func (d *JulesDriver) SetHTTPClient(client HTTPClient) {
	d.http = client
}

// julesSessionRequest is the Jules API POST body.
type julesSessionRequest struct {
	Title          string      `json:"title"`
	Prompt         string      `json:"prompt"`
	SourceContext  julesSource `json:"sourceContext"`
	AutomationMode string      `json:"automationMode"`
}

type julesSource struct {
	Source            string         `json:"source"`
	GithubRepoContext julesGithubCtx `json:"githubRepoContext"`
}

type julesGithubCtx struct {
	StartingBranch string `json:"startingBranch"`
}

// Send creates a Jules session using the prompt and title resolved by CodeJob.
// Jules accesses the referenced file directly from the repository via its GitHub App access.
// If the source is not yet indexed in Jules (404 on new repos), it polls GET /sources
// until the source appears or the timeout is exceeded.
func (d *JulesDriver) Send(prompt, title string) (string, error) {
	if err := gitmod.EnsureGHSession(d.runner, keyring.OpenKeyring("devflow")); err != nil {
		return "", err
	}
	apiKey, err := d.resolveAPIKey()
	if err != nil {
		return "", err
	}

	// Resolve candidate source IDs (might be multiple if rename occurred)
	candidateSourceIDs, err := d.resolveSourceID()
	if err != nil {
		return "", err
	}

	branch, err := d.resolveBranch()
	if err != nil {
		return "", err
	}

	if d.config.SessionTitle != "" {
		title = d.config.SessionTitle // config override takes precedence
	}
	if title == "" {
		title = "CodeJob Task" // ultimate fallback
	}

	// Helper to attempt creation with a specific sourceID
	attemptCreate := func(sourceID string) (int, []byte, error) {
		body := julesSessionRequest{
			Title:  title,
			Prompt: prompt,
			SourceContext: julesSource{
				Source: sourceID,
				GithubRepoContext: julesGithubCtx{
					StartingBranch: branch,
				},
			},
			AutomationMode: "AUTO_CREATE_PR",
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("could not encode request: %w", err)
		}

		req, err := http.NewRequest(http.MethodPost,
			"https://jules.googleapis.com/v1alpha/sessions", bytes.NewReader(payload))
		if err != nil {
			return 0, nil, fmt.Errorf("could not create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Goog-Api-Key", apiKey)
		resp, err := d.http.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("Jules API request failed: %w", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b, nil
	}

	// Try candidates in order
	var lastCode int
	var lastResp []byte
	var activeSourceID string

	for i, sourceID := range candidateSourceIDs {
		activeSourceID = sourceID
		code, respBody, err := attemptCreate(sourceID)
		if err != nil {
			return "", err
		}
		lastCode = code
		lastResp = respBody

		if code == http.StatusOK {
			return d.parseSessionID(respBody)
		}

		// If 404 and we have another candidate, try next
		if code == http.StatusNotFound && i < len(candidateSourceIDs)-1 {
			d.log(fmt.Sprintf("Jules: 404 on %s, falling back to next candidate...", sourceID))
			continue
		}

		// If failure (non-404) or last candidate, stop loop and handle error/polling
		break
	}

	if lastCode != http.StatusNotFound {
		return "", fmt.Errorf("Jules API returned %d: %s", lastCode, strings.TrimSpace(string(lastResp)))
	}

	// Fallback Polling Logic (using activeSourceID - the last one tried)
	// 404: check whether the source is simply not indexed yet.
	indexed, err := d.isSourceIndexed(activeSourceID, apiKey)
	if err != nil {
		return "", fmt.Errorf("Jules source check failed: %w", err)
	}
	if indexed {
		// Source exists in Jules but the API still returned 404 — real error.
		return "", fmt.Errorf("Jules API returned 404: %s", strings.TrimSpace(string(lastResp)))
	}

	// Source not indexed yet — poll until it appears or timeout is exceeded.
	timeout := d.config.SourceIndexTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	interval := d.config.SourceIndexInterval
	if interval == 0 {
		interval = 10 * time.Second
	}
	d.log("Jules: source not indexed yet, waiting for", activeSourceID)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		time.Sleep(interval)
		found, err := d.isSourceIndexed(activeSourceID, apiKey)
		if err != nil {
			return "", fmt.Errorf("Jules source check failed: %w", err)
		}
		if !found {
			d.log("Jules: source still not indexed, retrying...")
			continue
		}
		d.log("Jules: source now indexed, retrying session...")
		code, respBody, err := attemptCreate(activeSourceID)
		if err != nil {
			return "", err
		}
		if code == http.StatusOK {
			return d.parseSessionID(respBody)
		}
		return "", fmt.Errorf("Jules API returned %d after source appeared: %s",
			code, strings.TrimSpace(string(respBody)))
	}

	return "", fmt.Errorf("Jules source %q not indexed after %s — add the repo to Jules and retry",
		activeSourceID, timeout)
}

// parseSessionID extracts the session ID from a Jules 200 response body.
func (d *JulesDriver) parseSessionID(respBody []byte) (string, error) {
	var julesResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(respBody, &julesResp)
	d.sessionID = julesResp.ID
	return JulesResultPrefix + d.sessionID, nil
}

// isSourceIndexed paginates GET /v1alpha/sources and returns true if sourceID is found.
func (d *JulesDriver) isSourceIndexed(sourceID, apiKey string) (bool, error) {
	pageToken := ""
	for {
		url := "https://jules.googleapis.com/v1alpha/sources"
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, fmt.Errorf("could not create sources request: %w", err)
		}
		req.Header.Set("X-Goog-Api-Key", apiKey)

		resp, err := d.http.Do(req)
		if err != nil {
			return false, fmt.Errorf("Jules sources request failed: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var sr struct {
			Sources []struct {
				Name string `json:"name"`
			} `json:"sources"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &sr); err != nil {
			return false, fmt.Errorf("Jules sources parse error: %w", err)
		}
		for _, s := range sr.Sources {
			if s.Name == sourceID {
				return true, nil
			}
		}
		if sr.NextPageToken == "" {
			return false, nil
		}
		pageToken = sr.NextPageToken
	}
}

// resolveAPIKey returns config.APIKey or fetches it from keyring/prompt.
func (d *JulesDriver) resolveAPIKey() (string, error) {
	if d.config.APIKey != "" {
		return d.config.APIKey, nil
	}
	auth, err := NewJulesAuth()
	if err != nil {
		return "", fmt.Errorf("could not initialize keyring: %w", err)
	}
	auth.SetLog(d.log)
	return auth.EnsureAPIKey()
}

// resolveSourceID returns config.SourceID or auto-detects via gh CLI.
// Returns a list of candidate source IDs to try (primary + fallback).
func (d *JulesDriver) resolveSourceID() ([]string, error) {
	if d.config.SourceID != "" {
		return []string{d.config.SourceID}, nil
	}
	return getCandidateOrigins()
}

// resolveBranch returns config.StartBranch or auto-detects via git.
func (d *JulesDriver) resolveBranch() (string, error) {
	if d.config.StartBranch != "" {
		return d.config.StartBranch, nil
	}
	return autoDetectBranch()
}

// getLocalGitOrigin parses `git remote -v` to find the fetch URL for origin.
// Supports:
//   - HTTPS: https://github.com/owner/repo.git
//   - SSH:   git@github.com:owner/repo.git
func getLocalGitOrigin() (owner, repo string, err error) {
	out, err := command.Run("git", "remote", "-v")
	if err != nil {
		return "", "", fmt.Errorf("git remote failed: %w", err)
	}

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		// Expect: origin  https://github.com/owner/repo.git (fetch)
		if !strings.Contains(line, "origin") || !strings.Contains(line, "(fetch)") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		url := parts[1]

		// Cleanup .git suffix
		url = strings.TrimSuffix(url, ".git")

		// Parse HTTPS
		if strings.HasPrefix(url, "https://github.com/") {
			path := strings.TrimPrefix(url, "https://github.com/")
			parts := strings.Split(path, "/")
			if len(parts) == 2 {
				return parts[0], parts[1], nil
			}
		}

		// Parse SSH
		if strings.HasPrefix(url, "git@github.com:") {
			path := strings.TrimPrefix(url, "git@github.com:")
			parts := strings.Split(path, "/")
			if len(parts) == 2 {
				return parts[0], parts[1], nil
			}
		}
	}
	return "", "", fmt.Errorf("could not parse origin URL from: %s", out)
}

// getCandidateOrigins returns a list of source IDs.
// 1. Primary: From `gh repo view` (API source of truth).
// 2. Fallback: From `git remote -v` (Local config, may differ after rename).
func getCandidateOrigins() ([]string, error) {
	candidates := []string{}

	// 1. Try gh repo view (Primary)
	ghOwner, ghRepo, ghErr := autoDetectOwnerRepo()
	if ghErr == nil {
		id := "sources/github/" + ghOwner + "/" + ghRepo
		candidates = append(candidates, id)
	}

	// 2. Try git remote -v (Fallback)
	gitOwner, gitRepo, gitErr := getLocalGitOrigin()
	if gitErr == nil {
		id := "sources/github/" + gitOwner + "/" + gitRepo
		// Only append if different from primary
		isDuplicate := false
		for _, c := range candidates {
			if c == id {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			candidates = append(candidates, id)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("could not detect any GitHub repo source. gh error: %v, git error: %v", ghErr, gitErr)
	}

	return candidates, nil
}

// autoDetectOwnerRepo uses gh CLI to return the GitHub owner and repo name.
func autoDetectOwnerRepo() (owner, repo string, err error) {
	out, err := command.Run("gh", "repo", "view", "--json", "owner,name")
	if err != nil {
		return "", "", fmt.Errorf("could not detect GitHub repo (is gh CLI installed?): %w", err)
	}
	var r struct {
		Owner struct{ Login string } `json:"owner"`
		Name  string                 `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return "", "", fmt.Errorf("could not parse repo info: %w", err)
	}
	if r.Owner.Login == "" || r.Name == "" {
		return "", "", fmt.Errorf("incomplete repo info from gh: %s", out)
	}
	return r.Owner.Login, r.Name, nil
}

// autoDetectBranch uses git to get the current branch name.
func autoDetectBranch() (string, error) {
	branch, err := command.Run("git", "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("could not detect git branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "main", nil
	}
	return branch, nil
}
