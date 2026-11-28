package devflow_test

import (
	"strings"
	"testing"
)

// uiTimerModule is a module whose main package draws with webtyp.com/dom and arms a timer with timerCall. dom and
// time are small local stand-ins (replace), so the test needs no network.
func uiTimerModule(timerCall string) map[string]string {
	return map[string]string{
		"go.mod": "module example.com/styletest\n\ngo 1.20\n\nrequire (\n\twebtyp.com/dom v0.0.0\n\twebtyp.com/time v0.0.0\n)\n\n" +
			"replace webtyp.com/dom => ./fakedom\n\nreplace webtyp.com/time => ./faketime\n",
		"faketime/go.mod": "module webtyp.com/time\n\ngo 1.20\n",
		"faketime/time.go": "package time\n\ntype Timer interface{ Stop() bool }\n\nfunc AfterFunc(ms int, f func()) Timer { return nil }\n",
		"fakedom/go.mod": "module webtyp.com/dom\n\ngo 1.20\n\nrequire webtyp.com/time v0.0.0\n\nreplace webtyp.com/time => ../faketime\n",
		"fakedom/dom.go": "package dom\n\nimport \"webtyp.com/time\"\n\nfunc AfterFunc(ms int, f func()) (time.Timer, bool) { return nil, false }\n\nfunc Draw() {}\n",
		"main.go": "package main\n\nimport (\n\t\"webtyp.com/dom\"\n\t\"webtyp.com/time\"\n)\n\nvar _ time.Timer\n\n" +
			"func main() {\n\tdom.Draw()\n\t" + timerCall + "\n}\n",
	}
}

// Interface code (a package that imports webtyp.com/dom) arming time.AfterFunc fails the style step: on the server
// that callback runs on another goroutine while the page is drawn with the same signals.
func TestStyle_InterfaceTimerFromTimeFails(t *testing.T) {
	summary, console := runStyleFiles(t, uiTimerModule(`time.AfterFunc(10, func() {})`))
	if !strings.Contains(summary, "style ❌") {
		t.Fatalf("time.AfterFunc in interface code must fail the style step, summary: %q\nconsole:\n%s", summary, console)
	}
	if !strings.Contains(console, "use dom.AfterFunc") {
		t.Errorf("the message must say what to use instead, console:\n%s", console)
	}
}

// The same timer through dom.AfterFunc passes.
func TestStyle_InterfaceTimerFromDomPasses(t *testing.T) {
	summary, console := runStyleFiles(t, uiTimerModule(`dom.AfterFunc(10, func() {})`))
	if strings.Contains(summary, "style ❌") {
		t.Fatalf("dom.AfterFunc must pass the style step, summary: %q\nconsole:\n%s", summary, console)
	}
	if !strings.Contains(summary, "style ✅") {
		t.Fatalf("the style step must have run and passed, summary: %q\nconsole:\n%s", summary, console)
	}
}
