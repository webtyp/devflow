package devflow_test

import (
	"encoding/json"
	"webtyp.com/command"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"webtyp.com/devflow"
)

// mockHTTPClient is a test double for devflow.HTTPClient.
type mockHTTPClient struct {
	statusCode int
	body       string
	err        error
	lastReq    *http.Request
	lastBody   []byte
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.lastReq = req
	if req.Body != nil {
		m.lastBody, _ = io.ReadAll(req.Body)
	}
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

func testJulesConfig() devflow.JulesConfig {
	return devflow.JulesConfig{
		APIKey:      "test-key",
		SourceID:    "sources/github/user/repo",
		StartBranch: "main",
	}
}

func TestJulesDriverSendSuccess(t *testing.T) {
	d := devflow.NewJulesDriver(testJulesConfig())
	d.SetHTTPClient(&mockHTTPClient{statusCode: 200, body: `{"id":"S123"}`})
	d.SetRunner(&mockRunner{})

	result, err := d.Send("Execute the implementation plan described in docs/PLAN.md", "user/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "jules: S123") {
		t.Errorf("unexpected result: %s", result)
	}
	if d.SessionID() != "S123" {
		t.Errorf("expected session ID S123, got %q", d.SessionID())
	}
}

func TestJulesDriverSendNon200(t *testing.T) {
	d := devflow.NewJulesDriver(testJulesConfig())
	d.SetHTTPClient(&mockHTTPClient{statusCode: 403, body: "forbidden"})
	d.SetRunner(&mockRunner{})

	_, err := d.Send("Execute the implementation plan described in docs/PLAN.md", "")
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

func TestJulesDriverSendUsesProvidedAPIKey(t *testing.T) {
	mock := &mockHTTPClient{statusCode: 200, body: "{}"}
	d := devflow.NewJulesDriver(testJulesConfig())
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	d.Send("Execute the implementation plan described in docs/PLAN.md", "")

	if mock.lastReq == nil {
		t.Fatal("no request was made")
	}
	if got := mock.lastReq.Header.Get("X-Goog-Api-Key"); got != "test-key" {
		t.Errorf("expected API key %q, got %q", "test-key", got)
	}
}

func TestJulesDriverSendUsesReceivedPrompt(t *testing.T) {
	const customPrompt = "Execute the implementation plan described in docs/PLAN.md"
	mock := &mockHTTPClient{statusCode: 200, body: "{}"}
	d := devflow.NewJulesDriver(testJulesConfig())
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	d.Send(customPrompt, "")

	if mock.lastBody == nil {
		t.Fatal("no request body captured")
	}

	var payload map[string]any
	if err := json.Unmarshal(mock.lastBody, &payload); err != nil {
		t.Fatalf("could not decode request body: %v", err)
	}

	got, ok := payload["prompt"].(string)
	if !ok || got == "" {
		t.Fatal("prompt field missing or empty in request body")
	}
	if got != customPrompt {
		t.Errorf("driver must use the prompt it received\nwant: %q\n got: %q", customPrompt, got)
	}
}

func TestJulesDriverSendUsesTitle(t *testing.T) {
	mock := &mockHTTPClient{statusCode: 200, body: "{}"}
	d := devflow.NewJulesDriver(testJulesConfig())
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	d.Send("Execute the plan", "myorg/myrepo")

	var payload map[string]any
	if err := json.Unmarshal(mock.lastBody, &payload); err != nil {
		t.Fatalf("could not decode request body: %v", err)
	}
	if got, _ := payload["title"].(string); got != "myorg/myrepo" {
		t.Errorf("expected title %q in request body, got %q", "myorg/myrepo", got)
	}
}

func TestJulesDriverSendConfigTitleOverrides(t *testing.T) {
	mock := &mockHTTPClient{statusCode: 200, body: "{}"}
	cfg := testJulesConfig()
	cfg.SessionTitle = "custom title"
	d := devflow.NewJulesDriver(cfg)
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	d.Send("Execute the plan", "myorg/myrepo") // title arg should be ignored

	var payload map[string]any
	if err := json.Unmarshal(mock.lastBody, &payload); err != nil {
		t.Fatalf("could not decode request body: %v", err)
	}
	if got, _ := payload["title"].(string); got != "custom title" {
		t.Errorf("expected config SessionTitle to override, got %q", got)
	}
}

// --- sequential mock for multi-call tests ---

type seqResponse struct {
	statusCode int
	body       string
}

type mockHTTPClientSeq struct {
	responses []seqResponse
	calls     int
}

func (m *mockHTTPClientSeq) Do(req *http.Request) (*http.Response, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1 // repeat last response
	}
	r := m.responses[idx]
	return &http.Response{
		StatusCode: r.statusCode,
		Body:       io.NopCloser(strings.NewReader(r.body)),
	}, nil
}

// julesSourcesBody builds a minimal Jules sources JSON response.
func julesSourcesBody(names ...string) string {
	type src struct {
		Name string `json:"name"`
	}
	type resp struct {
		Sources []src `json:"sources"`
	}
	r := resp{}
	for _, n := range names {
		r.Sources = append(r.Sources, src{Name: n})
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestJulesDriverSendRetriesWhenSourceNotIndexed(t *testing.T) {
	const sourceID = "sources/github/user/repo"
	cfg := devflow.JulesConfig{
		APIKey:              "test-key",
		SourceID:            sourceID,
		StartBranch:         "main",
		SourceIndexTimeout:  200 * time.Millisecond,
		SourceIndexInterval: 10 * time.Millisecond,
	}
	mock := &mockHTTPClientSeq{
		responses: []seqResponse{
			{404, "not found"},                // [0] POST /sessions → 404
			{200, julesSourcesBody()},         // [1] GET /sources → empty (not indexed yet)
			{200, julesSourcesBody(sourceID)}, // [2] GET /sources → source appears
			{200, `{"id":"S999"}`},            // [3] POST /sessions → success
		},
	}
	d := devflow.NewJulesDriver(cfg)
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	result, err := d.Send("Execute the plan", "user/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "jules: S999") {
		t.Errorf("expected session ID S999 in result, got: %s", result)
	}
}

func TestJulesDriverSendReturns404WhenSourceIndexedButStill404(t *testing.T) {
	const sourceID = "sources/github/user/repo"
	cfg := devflow.JulesConfig{
		APIKey:      "test-key",
		SourceID:    sourceID,
		StartBranch: "main",
	}
	mock := &mockHTTPClientSeq{
		responses: []seqResponse{
			{404, "real api error"},           // [0] POST /sessions → 404
			{200, julesSourcesBody(sourceID)}, // [1] GET /sources → source IS indexed
		},
	}
	d := devflow.NewJulesDriver(cfg)
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	_, err := d.Send("Execute the plan", "")
	if err == nil {
		t.Fatal("expected error when source is indexed but Jules returns 404")
	}
	if !strings.Contains(err.Error(), "Jules API returned 404") {
		t.Errorf("expected '404' in error, got: %v", err)
	}
}

func TestJulesDriverSendTimesOutIfSourceNeverAppears(t *testing.T) {
	cfg := devflow.JulesConfig{
		APIKey:              "test-key",
		SourceID:            "sources/github/user/repo",
		StartBranch:         "main",
		SourceIndexTimeout:  25 * time.Millisecond,
		SourceIndexInterval: 10 * time.Millisecond,
	}
	// All GET /sources calls return an empty list — source never appears.
	mock := &mockHTTPClientSeq{
		responses: []seqResponse{
			{404, "not found"},        // [0] POST /sessions → 404
			{200, julesSourcesBody()}, // [1..N] GET /sources → always empty
		},
	}
	d := devflow.NewJulesDriver(cfg)
	d.SetHTTPClient(mock)
	d.SetRunner(&mockRunner{})

	_, err := d.Send("Execute the plan", "")
	if err == nil {
		t.Fatal("expected timeout error when source never appears")
	}
	if !strings.Contains(err.Error(), "not indexed after") {
		t.Errorf("expected 'not indexed after' in error, got: %v", err)
	}
}

func TestJulesDriverResolvesCandidates(t *testing.T) {
	// Mock ExecCommand to simulate gh repo view and git remote -v
	origExec := command.Exec
	defer func() { command.Exec = origExec }()

	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		if name == "gh" && strings.Contains(full, "repo view") {
			// Simulate GH returning new repo name
			return exec.Command("echo", `{"owner":{"login":"neworg"},"name":"newrepo"}`)
		}
		if name == "git" && strings.Contains(full, "remote -v") {
			// Simulate git returning old repo name
			return exec.Command("echo", `origin	https://github.com/oldorg/oldrepo.git (fetch)
origin	https://github.com/oldorg/oldrepo.git (push)`)
		}
		if name == "git" && strings.Contains(full, "branch --show-current") {
			return exec.Command("echo", "main")
		}
		return exec.Command("true")
	}

	// Mock HTTP client to verify which sourceID is used
	// We expect first attempt (neworg/newrepo) to 404,
	// then fallback (oldorg/oldrepo) to 200.
	mock := &mockHTTPClientSeq{
		responses: []seqResponse{
			{404, "not found"},     // [0] neworg/newrepo -> 404
			{200, `{"id":"S999"}`}, // [1] oldorg/oldrepo -> 200
		},
	}

	cfg := devflow.JulesConfig{
		APIKey:      "test-key",
		StartBranch: "main",
		// No SourceID provided, so it must auto-detect
	}
	d := devflow.NewJulesDriver(cfg)
	d.SetHTTPClient(mock)

	result, err := d.Send("Execute plan", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "jules: S999") {
		t.Errorf("expected session ID S999, got: %s", result)
	}

	// Verify that we actually made two requests
	if mock.calls != 2 {
		t.Errorf("expected 2 HTTP calls (primary + fallback), got %d", mock.calls)
	}
}
