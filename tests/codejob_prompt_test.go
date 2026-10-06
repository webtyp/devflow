package devflow_test

import (
	"strings"
	"testing"

	gitmod "webtyp.com/git"

	"webtyp.com/devflow"
)

// promptDriver records the prompt the executor receives.
type promptDriver struct{ prompt string }

func (p *promptDriver) Name() string          { return "prompt" }
func (p *promptDriver) SetLog(_ func(...any)) {}
func (p *promptDriver) Send(prompt, _ string) (string, error) {
	p.prompt = prompt
	return "ok", nil
}

// The executor must finish the plan and open the PR without stopping to ask: a question
// pauses the session until a human answers, and the plan loop stalls.
func TestCodeJob_Send_PromptForbidsStoppingToAsk(t *testing.T) {
	path := writeTempFile(t, "---\nPLAN: test\n---\nsome plan")
	d := &promptDriver{}
	job := devflow.NewCodeJob(d)
	job.SetRunner(&mockRunner{})
	job.SetPublisher(&MockPublisher{PublishFn: func(string, string, bool, bool, bool, bool, bool, bool) (gitmod.PushResult, error) {
		return gitmod.PushResult{}, nil
	}})

	if _, err := job.Send(path); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{path, "Never ask", "pull request", "Executor notes", "Never edit the frontmatter"} {
		if !strings.Contains(d.prompt, want) {
			t.Errorf("dispatch prompt lacks %q:\n%s", want, d.prompt)
		}
	}
}
