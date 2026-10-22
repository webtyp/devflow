package devflow

import (
	"bytes"
	"context"
	"os"
	"strings"
)

const (
	edgeMainDir      = "web/edge"    // web/edge/main.go: the Worker main
	browserMainDir   = "web/browser" // web/browser/main.go: the browser main
	mainFile         = "main.go"
	browserPassLabel = "browser"
	workerPassLabel  = "worker"
)

func buildTagsArg(runAll bool, extraTag string) string {
	var tags []string
	if runAll {
		tags = append(tags, "integration")
	}
	if extraTag != "" {
		tags = append(tags, extraTag)
	}
	if len(tags) > 0 {
		return "-tags=" + strings.Join(tags, ",")
	}
	return ""
}

func (g *Go) runBrowserTests(dir string, runAll bool, timeoutSec int, extraArgs []string) (output string, failed bool) {
	args := []string{"test", "-exec", g.wasmExecArg(), "-count=1"}
	if tags := buildTagsArg(runAll, browserTag); tags != "" {
		args = append(args, tags)
	}
	args = append(args, extraArgs...)
	args = append(args, g.wasmTestPackages(dir, runAll, browserTag)...)

	ctx, cancel := context.WithTimeout(context.Background(), g.wasmTimeout(timeoutSec))
	defer cancel()

	cmd := GoTestCmdFn(ctx, dir, "go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")

	var out bytes.Buffer
	filter := NewConsoleFilter(g.consoleOutput)
	pipe := &paramWriter{
		write: func(p []byte) (n int, err error) {
			s := string(p)
			out.Write(p)
			filter.Add(s)
			return len(p), nil
		},
	}
	cmd.Stdout = pipe
	cmd.Stderr = pipe

	err := cmd.Run()
	failed = err != nil || ctx.Err() == context.DeadlineExceeded

	return out.String(), failed
}

func (g *Go) vetWebMain(pkgDir, tag string) (output string, failed bool) {
	var args []string
	if tag == "" {
		args = []string{"vet", "./" + pkgDir}
	} else {
		args = []string{"vet", "-tags=" + tag, "./" + pkgDir}
	}

	cmd := GoTestCmdFn(context.Background(), g.rootDir, "go", args...)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	failed = err != nil

	return out.String(), failed
}
