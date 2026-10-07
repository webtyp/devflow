package devflow_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

// routedJulesClient answers each Jules API call by "METHOD URL" and records the
// calls with their bodies, so a test can check what codejob sent.
type routedJulesClient struct {
	routes map[string]string // "GET https://..." → JSON body
	calls  []string          // "METHOD URL BODY"
}

func (c *routedJulesClient) Do(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.String()
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	c.calls = append(c.calls, strings.TrimSpace(key+" "+body))
	if req.Header.Get("X-Goog-Api-Key") != "key" {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	resp, ok := c.routes[key]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(resp))}, nil
}

const (
	julesSession    = "https://jules.googleapis.com/v1alpha/sessions/S1"
	julesActivities = julesSession + "/activities?pageSize=100"
	// Oldest first, as the Jules API returns them; the question is the agent's latest message.
	activitiesJSON = `{"activities":[
		{"originator":"agent","agentMessaged":{"agentMessage":"old question"}},
		{"originator":"user","userMessaged":{"userMessage":"an answer"}},
		{"originator":"agent","agentMessaged":{"agentMessage":"Should I delete Tilde too?"}},
		{"originator":"agent","progressUpdated":{"title":"running tests"}}
	]}`
)

// A stopped session is only actionable if the developer can read what Jules
// asked: the status message carries the agent's latest message and the command
// that answers it.
func TestJulesSessionState_StoppedSessionShowsTheQuestion(t *testing.T) {
	cases := []struct {
		state, want string
	}{
		{"AWAITING_USER_FEEDBACK", "codejob --reply"},
		{"AWAITING_PLAN_APPROVAL", "codejob --approve"},
		{"COMPLETED", "codejob --reply"},
		{"FAILED", "session failed"},
	}
	for _, c := range cases {
		client := &routedJulesClient{routes: map[string]string{
			"GET " + julesSession:    `{"id":"S1","state":"` + c.state + `","outputs":[]}`,
			"GET " + julesActivities: activitiesJSON,
		}}
		msg, prURL, done, err := devflow.JulesSessionState("S1", "key", client)
		if err != nil {
			t.Fatalf("%s: %v", c.state, err)
		}
		if done || prURL != "" {
			t.Errorf("%s: no PR yet, got done=%v pr=%q", c.state, done, prURL)
		}
		if strings.Contains(msg, "working") {
			t.Errorf("%s: a stopped session is not working: %q", c.state, msg)
		}
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: want %q in %q", c.state, c.want, msg)
		}
		if !strings.Contains(msg, "Should I delete Tilde too?") || strings.Contains(msg, "old question") {
			t.Errorf("%s: want only the latest agent message in %q", c.state, msg)
		}
	}
}

// A completed session without a PR is the case seen with devbrowser and fmt:
// it never produces a PR on its own, so it must not read as "working".
func TestJulesSessionState_CompletedWithoutPRIsNotWorking(t *testing.T) {
	client := &routedJulesClient{routes: map[string]string{
		"GET " + julesSession:    `{"id":"S1","state":"COMPLETED","outputs":[]}`,
		"GET " + julesActivities: `{"activities":[]}`,
	}}
	msg, _, done, err := devflow.JulesSessionState("S1", "key", client)
	if err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if !strings.Contains(msg, "without a PR") || !strings.Contains(msg, "S1") {
		t.Errorf("got %q", msg)
	}
}

func TestJulesSendMessage(t *testing.T) {
	client := &routedJulesClient{routes: map[string]string{
		"POST " + julesSession + ":sendMessage": `{}`,
	}}
	if err := devflow.JulesSendMessage("S1", "key", `finish "Stage 2" and open the PR`, client); err != nil {
		t.Fatal(err)
	}
	want := `POST ` + julesSession + `:sendMessage {"prompt":"finish \"Stage 2\" and open the PR"}`
	if len(client.calls) != 1 || client.calls[0] != want {
		t.Errorf("want %s, got %v", want, client.calls)
	}

	if err := devflow.JulesSendMessage("S1", "key", "  ", client); err == nil {
		t.Error("an empty reply must be rejected before calling Jules")
	}
}

func TestJulesApprovePlan(t *testing.T) {
	client := &routedJulesClient{routes: map[string]string{
		"POST " + julesSession + ":approvePlan": `{}`,
	}}
	if err := devflow.JulesApprovePlan("S1", "key", client); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || !strings.HasPrefix(client.calls[0], "POST "+julesSession+":approvePlan") {
		t.Errorf("got %v", client.calls)
	}

	if err := devflow.JulesApprovePlan("S1", "bad", client); err == nil {
		t.Error("a non-200 answer must be an error")
	}
}

func TestParseCodeJobFlags_ReplyAndApprove(t *testing.T) {
	opts := devflow.ParseCodeJobFlags([]string{"codejob", "--reply", "open the PR"})
	if opts.Reply != "open the PR" || opts.Message != "" {
		t.Errorf("--reply: %+v", opts)
	}
	opts = devflow.ParseCodeJobFlags([]string{"codejob", "--reply=yes, delete it"})
	if opts.Reply != "yes, delete it" {
		t.Errorf("--reply=: %+v", opts)
	}
	opts = devflow.ParseCodeJobFlags([]string{"codejob", "--approve"})
	if !opts.Approve || opts.Message != "" {
		t.Errorf("--approve: %+v", opts)
	}
}
