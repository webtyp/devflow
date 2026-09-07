package devflow

import (
	"bytes"
	"context"
	"fmt"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var semverTagRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// GoTestCmdFn creates the command used to run 'go test'. Override in tests to avoid
// launching a real nested go test subprocess (e.g. from Release→Push→Test).
var GoTestCmdFn = testCommand

// TestOptions configures a test run. The zero value runs the full suite with
// the race detector, the default timeout, and the build cache enabled.
type TestOptions struct {
	Args     []string // extra `go test` arguments; nil or empty = full suite
	SkipRace bool     // omit -race
	Timeout  int      // seconds; 0 = the package default
	NoCache  bool     // add -count=1
	RunAll   bool     // include the packages normally skipped
}

// Test executes the test suite for the project. The zero TestOptions runs the
// full suite with -race, the default timeout, and the build cache enabled.
func (g *Go) Test(opts TestOptions) (string, error) {
	customArgs := opts.Args
	skipRace := opts.SkipRace
	timeoutSec := opts.Timeout
	noCache := opts.NoCache
	runAll := opts.RunAll

	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	hasCustomArgs := len(customArgs) > 0

	// Detect Module Name
	moduleName, err := getModuleName(g.rootDir)
	if err != nil {
		return "", fmt.Errorf("error: %v", err)
	}

	// Check cache only for full suite runs
	if !hasCustomArgs && !noCache {
		cache := gitmod.NewTestCache(g.rootDir)
		if cache.IsCacheValid() {
			return cache.GetCachedMessage(), nil
		}
	}

	// Branch based on whether custom args are provided
	if hasCustomArgs {
		return g.runCustomTests(customArgs, moduleName, timeoutSec, runAll)
	}

	// Full test suite (run all phases)
	return g.runFullTestSuite(moduleName, skipRace, timeoutSec, noCache, runAll)
}

// runFullTestSuite executes the complete test suite (vet, race, cover, wasm, badges)
func (g *Go) runFullTestSuite(moduleName string, skipRace bool, timeoutSec int, noCache bool, runAll bool) (string, error) {
	// Check cache - if code hasn't changed since last successful test, return cached result
	if !noCache {
		cache := gitmod.NewTestCache(g.rootDir)
		if cache.IsCacheValid() {
			return cache.GetCachedMessage(), nil
		}
	}

	start := time.Now()

	// Initialize Status
	testStatus := "Failed"
	coveragePercent := "0"
	raceStatus := "Detected"
	vetStatus := "Issues"

	var msgs []string
	addMsg := func(ok bool, msg string) {
		symbol := "✅"
		if !ok {
			symbol = "❌"
		}
		msgs = append(msgs, fmt.Sprintf("%s %s", msg, symbol))
	}

	// Parallel Phase 1: Vet + WASM detection
	var wg1 sync.WaitGroup
	var vetOutput string
	var vetErr error
	var enableWasmTests bool

	wg1.Add(2)

	// Go Vet (async)
	go func() {
		defer wg1.Done()
		vetArgs := []string{"vet"}
		if runAll {
			vetArgs = append(vetArgs, "-tags=integration")
		}
		vetArgs = append(vetArgs, "./...")
		vetOutput, vetErr = command.RunInDir(g.rootDir, "go", vetArgs...)
	}()

	// Check for WASM test files (async)
	go func() {
		defer wg1.Done()
		// Check for WASM test files
		// We do NOT return early for runAll anymore, we scan to see if actual WASM files exist.

		// 1. Get native test files
		nativeArgs := []string{"list", "-f", "{{.ImportPath}} {{.TestGoFiles}} {{.XTestGoFiles}}"}
		if runAll {
			nativeArgs = append(nativeArgs, "-tags=integration")
		}
		nativeArgs = append(nativeArgs, "./...")
		nativeCmd := exec.Command("go", nativeArgs...)
		nativeCmd.Dir = g.rootDir
		nativeOut, _ := nativeCmd.CombinedOutput()

		// 2. Get WASM test files
		wasmArgs := []string{"list", "-f", "{{.ImportPath}} {{.TestGoFiles}} {{.XTestGoFiles}}"}
		if runAll {
			wasmArgs = append(wasmArgs, "-tags=integration")
		}
		wasmArgs = append(wasmArgs, "./...")
		wasmCmd := exec.Command("go", wasmArgs...)
		wasmCmd.Dir = g.rootDir
		wasmCmd.Env = os.Environ()
		wasmCmd.Env = append(wasmCmd.Env, "GOOS=js", "GOARCH=wasm")
		wasmOut, _ := wasmCmd.CombinedOutput()

		// 3. Decision logic
		enableWasmTests = ShouldEnableWasm(string(nativeOut), string(wasmOut))
	}()

	wg1.Wait()

	// Process vet results
	if vetErr != nil {
		// Check if it's just "no packages" error (WASM-only projects)
		if strings.Contains(vetOutput, "matched no packages") ||
			strings.Contains(vetOutput, "no packages to vet") ||
			strings.Contains(vetOutput, "build constraints exclude all Go files") {
			vetStatus = "OK"
			addMsg(true, "vet")
		} else {
			vetStatus = "Issues"
			// Filter unsafe.Pointer warnings
			lines := strings.Split(vetOutput, "\n")
			var filteredLines []string
			for _, line := range lines {
				if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") { // Ignore comments/empty
					continue
				}
				if !strings.Contains(line, "possible misuse of unsafe.Pointer") {
					filteredLines = append(filteredLines, line)
				}
			}

			if len(filteredLines) > 0 {
				addMsg(false, "vet")
			} else {
				vetStatus = "OK"
				addMsg(true, "vet")
			}
		}
	} else {
		vetStatus = "OK"
		addMsg(true, "vet")
	}

	// Run tests with coverage and optional race detection
	// go test ./... automatically discovers all packages with tests
	var testErr error
	var testOutput string

	// Watchdog and backstop timeout semantincs:
	// -t N now means "max N seconds per test stall"
	// backstop is 10x larger to catch overall package hangs
	timeoutFlag := fmt.Sprintf("-timeout=%ds", timeoutSec*10)

	tmpCovDir, _ := os.MkdirTemp("", "gotest-cov")
	defer os.RemoveAll(tmpCovDir)
	coverProfilePath := fmt.Sprintf("%s/cover.out", tmpCovDir)
	testArgs := []string{"test", "-v", "-cover", "-coverpkg=./...", fmt.Sprintf("-coverprofile=%s", coverProfilePath), "-count=1", timeoutFlag}

	if runAll {
		testArgs = append(testArgs, "-tags=integration")
	}

	testArgs = append(testArgs, "./...")
	if !skipRace {
		testArgs = append(testArgs[:1], append([]string{"-race"}, testArgs[1:]...)...)
	}

	// Watchdog setup
	testCtx, testCancel := context.WithCancel(context.Background())
	defer testCancel()

	var watchdogFired bool
	wd := NewWatchdog(time.Duration(timeoutSec)*time.Second, func() {
		watchdogFired = true
		testCancel()
	})
	wd.Start()
	defer wd.Stop()

	testCmd := GoTestCmdFn(testCtx, g.rootDir, "go", testArgs...)

	testBuffer := &bytes.Buffer{}

	testFilter := NewConsoleFilter(g.consoleOutput)

	testPipe := &paramWriter{
		write: func(p []byte) (n int, err error) {
			s := string(p)
			testBuffer.Write(p)
			testFilter.Add(s)
			wd.Add(s)
			return len(p), nil
		},
	}

	testCmd.Stdout = testPipe
	testCmd.Stderr = testPipe
	testErr = testCmd.Run()

	// Run tests in submodule directories (own go.mod — not reached by ./...)
	// Pass -coverpkg pointing to the parent module so coverage reflects the actual code under test.
	covPkgFlag := fmt.Sprintf("-coverpkg=%s/...", moduleName)
	for _, subDir := range findSubModuleDirs(g.rootDir) {
		subArgs := []string{"test", "-v", "-cover", covPkgFlag, "-count=1", timeoutFlag, "./..."}
		if !skipRace {
			subArgs = append([]string{"test", "-race"}, subArgs[1:]...)
		}
		if runAll {
			subArgs = append(subArgs[:len(subArgs)-1], "-tags=integration", "./...")
		}

		subCtx, subCancel := context.WithCancel(context.Background())
		defer subCancel()

		// Re-initialize watchdog for submodule run
		wd = NewWatchdog(time.Duration(timeoutSec)*time.Second, func() {
			watchdogFired = true
			subCancel()
		})
		wd.Start()

		subCmd := GoTestCmdFn(subCtx, subDir, "go", subArgs...)
		subCmd.Stdout = testPipe
		subCmd.Stderr = testPipe
		if err := subCmd.Run(); err != nil && testErr == nil {
			testErr = err
		}
		wd.Stop()
	}

	testFilter.Flush()

	testOutput = testBuffer.String()

	// Detect process-level timeout (killed by watchdog or backstop)
	if testCtx.Err() == context.Canceled && watchdogFired {
		culprits := wd.Culprits()
		if len(culprits) > 0 {
			for _, name := range culprits {
				addMsg(false, fmt.Sprintf("timeout: %s stalled >%ds (no progress)", name, timeoutSec))
			}
		} else {
			addMsg(false, fmt.Sprintf("timeout: stall detected (>%ds)", timeoutSec))
		}
		testStatus = "Failed"
	} else if testCtx.Err() == context.DeadlineExceeded {
		addMsg(false, fmt.Sprintf("timeout: package exceeded %ds total (backstop)", timeoutSec*10))
		testStatus = "Failed"
	}

	// Process test results
	var stdTestsRan bool
	testStatus, raceStatus, stdTestsRan, msgs = EvaluateTestResults(testErr, testOutput, moduleName, msgs, skipRace)

	// If no stdlib tests ran but we see exclusions, consider enabling WASM (if not already enabled)
	if !stdTestsRan {
		isExclusionError := strings.Contains(testOutput, "matched no packages") ||
			strings.Contains(testOutput, "build constraints exclude all Go files")
		if isExclusionError {
			enableWasmTests = true
			g.log("No stdlib tests matched/run (possibly WASM-only module), skipping stdlib tests...")
		}
	}

	// Process coverage results from the profile generated during the test run above
	if stdTestsRan {
		if cov := exactCoverageFromProfile(coverProfilePath); cov != "" && cov != "0" && cov != "0.0" {
			coveragePercent = cov
		} else {
			coveragePercent = calculateAverageCoverage(testOutput)
		}
	}

	// WASM Tests
	var wasmTestOutput string
	if enableWasmTests {

		if err := g.installWasmBrowserTest(); err != nil {

			addMsg(false, "WASM tests skipped (setup failed)")
		} else {
			execArg := g.wasmExecArg()
			// Add -count=1 to force cache bypass for WASM tests, consistent with native run
			testArgs := []string{"test", "-exec", execArg, "-v", "-cover", "-coverpkg=./...", "-count=1"}
			testArgs = append(testArgs, g.wasmTestPackages(runAll)...)

			// Add cushion for WASM tests too
			wasmCtx, wasmCancel := context.WithTimeout(context.Background(), g.wasmTimeout(timeoutSec))
			defer wasmCancel()
			wasmCmd := GoTestCmdFn(wasmCtx, g.rootDir, "go", testArgs...)
			wasmCmd.Env = os.Environ()
			wasmCmd.Env = append(wasmCmd.Env, "GOOS=js", "GOARCH=wasm")

			var wasmOut bytes.Buffer

			wasmFilter := NewConsoleFilter(g.consoleOutput)
			wasmPipe := &paramWriter{
				write: func(p []byte) (n int, err error) {
					s := string(p)
					wasmOut.Write(p)
					wasmFilter.Add(s)
					return len(p), nil
				},
			}

			wasmCmd.Stdout = wasmPipe
			wasmCmd.Stderr = wasmPipe

			err := wasmCmd.Run()
			wasmFilter.Flush()

			wOutput := wasmOut.String()
			wasmTestOutput = wOutput

			// Detect process-level timeout for WASM tests
			if wasmCtx.Err() == context.DeadlineExceeded {
				timedOut := FindTimedOutTests(wOutput)
				if len(timedOut) == 0 {
					// wasmbrowsertest buffers output: retry individually to find culprit
					timedOut = g.findWasmTimeoutCulprit(timeoutSec)
				}
				if len(timedOut) > 0 {
					for _, name := range timedOut {
						addMsg(false, fmt.Sprintf("timeout: %s (exceeded %ds)", name, timeoutSec))
					}
				} else {
					addMsg(false, fmt.Sprintf("timeout: wasm tests exceeded %ds", timeoutSec))
				}
				testStatus = "Failed"
			} else if err != nil {
				// WASM test failure - ConsoleFilter already filtered the output in quiet mode
				addMsg(false, "wasm")
				testStatus = "Failed"
			} else {
				addMsg(true, "wasm")
				if testStatus != "Failed" {
					testStatus = "Passing"
				}
				wCov := calculateAverageCoverage(wOutput)

				// Try exact coverage for WASM if possible (might need special handling for WASM env)
				// WASM tests are tricky because we use -exec wasmbrowsertest.
				// getExactCoverage can support it if we pass correct args.
				// But getExactCoverage implementation uses 'go test' which should respect GOOS/GOARCH from env.
				// Let's rely on calculateAverageCoverage for WASM for now unless we update getExactCoverage to support WASM env injection passed from here.
				// Actually, we can try getExactCoverage but we need to set Env.
				// For now, let's stick to parsing for WASM as it seems reliable (89.0 vs 89.0 from manual run was parsed correctly from go tool cover output in manual run)
				// Wait, manual run output "total: ... 89.0%".
				// The parsed output of `go test` usually doesn't show "total:" key unless using -coverprofile?
				// The output we parse is "coverage: 80.5% of statements".
				// So manual run showed 89.0% because I ran `go tool cover`.
				// `gotest` parsing only sees what `go test` emits.
				// If we want 89.0% here, we need getExactCoverage for WASM too.

				// Let's stick to simple parsing for WASM for now to avoid complexity with wasmbrowsertest + profile generation multiple times.
				// The user sees 76.7% vs 89% discrepancy mostly because Native tests were averaging 22 and 80.

				if wCov != "0" {
					wVal, _ := strconv.ParseFloat(wCov, 64)
					nVal, _ := strconv.ParseFloat(coveragePercent, 64)
					if wVal > nVal {
						coveragePercent = wCov
					}
				}
			}
		}
	}

	// Report consolidated coverage
	if coveragePercent != "0" {
		msgs = append(msgs, "coverage: "+coveragePercent+"%")
	}

	// Detect slowest test across stdlib and WASM outputs
	allTestOutput := testOutput + "\n" + wasmTestOutput
	if name, dur := FindSlowestTest(allTestOutput, 2.0); name != "" {
		g.consoleOutput(fmt.Sprintf("⚠️ slow: %s (%.1fs)", name, dur))
	}

	// Detect timed out tests
	if timedOut := FindTimedOutTests(allTestOutput); len(timedOut) > 0 {
		for _, name := range timedOut {
			addMsg(false, fmt.Sprintf("timeout: %s (exceeded %ds)", name, timeoutSec))
		}
	}

	// Return error if tests or vet failed
	summary := fmt.Sprintf("%s%s (%.1fs)", strings.Join(msgs, ", "), g.currentTagSuffix(), time.Since(start).Seconds())
	if testStatus == "Failed" || vetStatus == "Issues" {
		return summary, fmt.Errorf("%s", summary)
	}

	// Badges
	licenseType := "MIT"
	if checkFileExists("LICENSE") {
		// naive check
	}
	goVer := GetGoVersion()

	bh := NewBadges()
	bh.SetRootDir(g.rootDir)
	bh.SetLog(g.log)
	if err := bh.updateBadges("README.md", licenseType, goVer, testStatus, coveragePercent, raceStatus, vetStatus, true); err != nil {
		g.log("Warning: failed to update badges:", err)
	}

	// Save test cache on success (for gopush optimization)
	// We save even if noCache=true, because this was a valid run
	cache := gitmod.NewTestCache(g.rootDir)
	if err := cache.SaveCache(summary); err != nil {
		g.log("Warning: failed to save test cache:", err)
	}

	return summary, nil
}

// runCustomTests executes tests with custom go test flags (fast path)
// Skips vet, badges, and cache, but runs WASM tests if detected
func (g *Go) runCustomTests(customArgs []string, moduleName string, timeoutSec int, runAll bool) (string, error) {
	start := time.Now()
	var msgs []string
	addMsg := func(ok bool, msg string) {
		symbol := "✅"
		if !ok {
			symbol = "❌"
		}
		msgs = append(msgs, fmt.Sprintf("%s %s", msg, symbol))
	}

	// Detect WASM tests in parallel with stdlib tests preparation
	var wg sync.WaitGroup
	var enableWasmTests bool

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Check for WASM test files by comparing native vs WASM test file lists
		// if runAll is set, we still check existence but include integration tags in detection

		nativeArgs := []string{"list", "-f", "{{.ImportPath}} {{.TestGoFiles}} {{.XTestGoFiles}}"}
		if runAll {
			nativeArgs = append(nativeArgs, "-tags=integration")
		}
		nativeArgs = append(nativeArgs, "./...")
		nativeCmd := exec.Command("go", nativeArgs...)
		nativeCmd.Dir = g.rootDir
		nativeOut, _ := nativeCmd.CombinedOutput()

		wasmArgs := []string{"list", "-f", "{{.ImportPath}} {{.TestGoFiles}} {{.XTestGoFiles}}"}
		if runAll {
			wasmArgs = append(wasmArgs, "-tags=integration")
		}
		wasmArgs = append(wasmArgs, "./...")
		wasmCmd := exec.Command("go", wasmArgs...)
		wasmCmd.Dir = g.rootDir
		wasmCmd.Env = os.Environ()
		wasmCmd.Env = append(wasmCmd.Env, "GOOS=js", "GOARCH=wasm")
		wasmOut, _ := wasmCmd.CombinedOutput()

		enableWasmTests = ShouldEnableWasm(string(nativeOut), string(wasmOut))
	}()

	// Inject timeout if user didn't already pass -timeout
	// Use -v for watchdog to work, ConsoleFilter will suppress the noise
	if !HasVFlag(customArgs) {
		customArgs = append([]string{"-v"}, customArgs...)
	}

	// Parse custom timeout if present
	for _, arg := range customArgs {
		if strings.HasPrefix(arg, "-t=") {
			if t, err := strconv.Atoi(strings.TrimPrefix(arg, "-t=")); err == nil {
				timeoutSec = t
			}
		} else if strings.HasPrefix(arg, "-timeout=") {
			// Try to parse go's duration format
			dStr := strings.TrimPrefix(arg, "-timeout=")
			if d, err := time.ParseDuration(dStr); err == nil {
				timeoutSec = int(d.Seconds())
			}
		}
	}

	timeoutFlag := fmt.Sprintf("-timeout=%ds", timeoutSec*10)
	if !HasTimeoutFlag(customArgs) {
		customArgs = append(customArgs, timeoutFlag)
	}

	// Build command: go test <customArgs> ./...
	testArgs := append([]string{"test"}, customArgs...)
	if runAll {
		testArgs = append(testArgs, "-tags=integration")
	}
	testArgs = append(testArgs, "./...")

	customCtx, customCancel := context.WithCancel(context.Background())
	defer customCancel()

	var watchdogFired bool
	wd := NewWatchdog(time.Duration(timeoutSec)*time.Second, func() {
		watchdogFired = true
		customCancel()
	})
	wd.Start()
	defer wd.Stop()

	testCmd := GoTestCmdFn(customCtx, g.rootDir, "go", testArgs...)
	testBuffer := &bytes.Buffer{}

	// CRITICAL: Keep ConsoleFilter for clean output
	testFilter := NewConsoleFilter(g.consoleOutput)
	testPipe := &paramWriter{
		write: func(p []byte) (n int, err error) {
			s := string(p)
			testBuffer.Write(p)
			testFilter.Add(s)
			wd.Add(s)
			return len(p), nil
		},
	}

	testCmd.Stdout = testPipe
	testCmd.Stderr = testPipe
	testErr := testCmd.Run()

	// Run tests in submodule directories (own go.mod — not reached by ./...)
	for _, subDir := range findSubModuleDirs(g.rootDir) {
		subArgs := append([]string{"test"}, customArgs...)
		if runAll {
			subArgs = append(subArgs, "-tags=integration")
		}
		subArgs = append(subArgs, "./...")

		subCtx, subCancel := context.WithCancel(context.Background())
		defer subCancel()

		// Re-initialize watchdog for submodule run
		wd = NewWatchdog(time.Duration(timeoutSec)*time.Second, func() {
			watchdogFired = true
			subCancel()
		})
		wd.Start()

		subCmd := GoTestCmdFn(subCtx, subDir, "go", subArgs...)
		subCmd.Stdout = testPipe
		subCmd.Stderr = testPipe
		if err := subCmd.Run(); err != nil && testErr == nil {
			testErr = err
		}
		wd.Stop()
	}

	testFilter.Flush()

	testOutput := testBuffer.String()

	// Detect process-level timeout
	customTestStatus := "Failed"
	if customCtx.Err() == context.Canceled && watchdogFired {
		culprits := wd.Culprits()
		if len(culprits) > 0 {
			for _, name := range culprits {
				addMsg(false, fmt.Sprintf("timeout: %s stalled >%ds (no progress)", name, timeoutSec))
			}
		} else {
			addMsg(false, fmt.Sprintf("timeout: stall detected (>%ds)", timeoutSec))
		}
	} else if customCtx.Err() == context.DeadlineExceeded {
		addMsg(false, fmt.Sprintf("timeout: package exceeded %ds total (backstop)", timeoutSec*10))
	} else {
		customTestStatus = "" // Not a context-triggered failure
	}

	// Wait for WASM detection to complete
	wg.Wait()

	// Process stdlib test results (without race detection reporting)
	testStatus, _, stdTestsRan, msgs := EvaluateTestResults(testErr, testOutput, moduleName, msgs, false)
	if customTestStatus != "" {
		testStatus = customTestStatus
	}

	// Initialize coveragePercent for custom runs (not calculated for stdlib in fast path usually, but we need it for comparison)
	coveragePercent := "0"

	// Process coverage if std tests ran (fast path: use average from output)
	if stdTestsRan {
		coveragePercent = calculateAverageCoverage(testOutput)
	}

	// Remove "race detection ok" message since we're not forcing -race in custom args
	// (user can add -race explicitly if desired)
	var filteredMsgs []string
	for _, msg := range msgs {
		if !strings.Contains(msg, "race detection ok") {
			filteredMsgs = append(filteredMsgs, msg)
		}
	}
	msgs = filteredMsgs

	// If no stdlib tests ran but we see exclusions, consider enabling WASM
	if !stdTestsRan {
		isExclusionError := strings.Contains(testOutput, "matched no packages") ||
			strings.Contains(testOutput, "build constraints exclude all Go files")
		if isExclusionError {
			enableWasmTests = true
			g.log("No stdlib tests matched/run (possibly WASM-only module), attempting WASM tests...")
		}
	}

	// Run WASM tests with same custom args (excluding -race)
	if enableWasmTests {
		if err := g.installWasmBrowserTest(); err != nil {
			addMsg(false, "WASM tests skipped (setup failed)")
		} else {
			// Build WASM test command with custom args, filtering out -race (not supported in WASM)
			var wasmArgs []string
			for _, arg := range customArgs {
				if arg != "-race" {
					wasmArgs = append(wasmArgs, arg)
				}
			}
			// Inject timeout for WASM tests too
			if !HasTimeoutFlag(wasmArgs) {
				wasmArgs = append(wasmArgs, timeoutFlag)
			}

			// Always add -count=1 for WASM to enforce consistent behavior (no caching)
			// unless user already specified it.
			hasCount := false
			for _, arg := range wasmArgs {
				if strings.Contains(arg, "-count") {
					hasCount = true
					break
				}
			}
			if !hasCount {
				wasmArgs = append(wasmArgs, "-count=1")
			}

			wasmTestArgs := append([]string{"test", "-exec", "wasmbrowsertest"}, wasmArgs...)
			if runAll {
				wasmTestArgs = append(wasmTestArgs, "-tags=integration")
			}
			wasmTestArgs = append(wasmTestArgs, "./...")

			wasmCtx, wasmCancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec+10)*time.Second)
			defer wasmCancel()
			wasmCmd := GoTestCmdFn(wasmCtx, g.rootDir, "go", wasmTestArgs...)
			wasmCmd.Env = os.Environ()
			wasmCmd.Env = append(wasmCmd.Env, "GOOS=js", "GOARCH=wasm")

			var wasmOut bytes.Buffer
			wasmFilter := NewConsoleFilter(g.consoleOutput)
			wasmPipe := &paramWriter{
				write: func(p []byte) (n int, err error) {
					s := string(p)
					wasmOut.Write(p)
					wasmFilter.Add(s)
					return len(p), nil
				},
			}

			wasmCmd.Stdout = wasmPipe
			wasmCmd.Stderr = wasmPipe

			err := wasmCmd.Run()
			wasmFilter.Flush()

			if wasmCtx.Err() == context.DeadlineExceeded {
				wOutput := wasmOut.String()
				timedOut := FindTimedOutTests(wOutput)
				if len(timedOut) == 0 {
					timedOut = g.findWasmTimeoutCulprit(timeoutSec)
				}
				if len(timedOut) > 0 {
					for _, name := range timedOut {
						addMsg(false, fmt.Sprintf("timeout: %s (exceeded %ds)", name, timeoutSec))
					}
				} else {
					addMsg(false, fmt.Sprintf("timeout: wasm tests exceeded %ds", timeoutSec))
				}
				testStatus = "Failed"
			} else if err != nil {
				addMsg(false, "wasm")
				testStatus = "Failed"
			} else {
				wOutput := wasmOut.String()
				wCov := calculateAverageCoverage(wOutput)
				if wCov != "0" {
					wVal, _ := strconv.ParseFloat(wCov, 64)
					nVal, _ := strconv.ParseFloat(coveragePercent, 64)
					if wVal > nVal {
						coveragePercent = wCov
					}
				}
			}
		}
	}

	// Report consolidated coverage if available (and not 0)
	if coveragePercent != "0" {
		msgs = append(msgs, "coverage: "+coveragePercent+"%")
	}

	summary := fmt.Sprintf("%s%s (%.1fs)", strings.Join(msgs, ", "), g.currentTagSuffix(), time.Since(start).Seconds())
	if testStatus == "Failed" {
		return summary, fmt.Errorf("%s", summary)
	}

	// NO cache save, NO badges (as requested)
	return summary, nil
}

// currentTagSuffix returns " <latest-git-tag>" for appending to a summary line,
// or "" if no git handler is configured or no tag exists yet.
func (g *Go) currentTagSuffix() string {
	if g.git == nil {
		return ""
	}
	tag, err := g.git.GetLatestTag()
	if err != nil || tag == "" {
		return ""
	}
	return " " + tag
}

// findSubModuleDirs returns immediate subdirectories that contain their own go.mod.
// go test ./... does not cross module boundaries, so callers must run tests in each separately.
func findSubModuleDirs(rootDir string) []string {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(rootDir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "go.mod")); err == nil {
			dirs = append(dirs, sub)
		}
	}
	return dirs
}

// testCommand creates an exec.Cmd with graceful timeout handling.
// On timeout: sends SIGINT first (lets the process flush output), then SIGKILL after 5s.
func testCommand(ctx context.Context, dir string, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

type paramWriter struct {
	write func(p []byte) (n int, err error)
}

func (p *paramWriter) Write(b []byte) (n int, err error) {
	return p.write(b)
}

// FindSlowestTest parses -v test output and returns the name and duration of the slowest individual test
// across all packages if it exceeds the specified threshold.
func FindSlowestTest(output string, threshold float64) (string, float64) {
	// Parse individual test timing from -v output: --- PASS: TestName (2.00s)
	testRe := regexp.MustCompile(`--- (?:PASS|FAIL): (\S+) \((\d+(?:\.\d+)?)s\)`)
	var slowestName string
	var slowestTime float64

	for _, match := range testRe.FindAllStringSubmatch(output, -1) {
		t, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		if t > slowestTime {
			slowestName = match[1]
			slowestTime = t
		}
	}

	if slowestTime >= threshold {
		return slowestName, slowestTime
	}
	return "", 0
}

func calculateAverageCoverage(output string) string {
	lines := strings.Split(output, "\n")

	// Map to store max coverage per package
	// If package name is unknown, use unique key to treat as separate
	pkgCoverage := make(map[string]float64)

	// Regex to parse: ok package_name time coverage: X% of statements [in target_package]
	// We want to group by the TARGET package if specified ("in target_package"),
	// otherwise by the test package.
	// Actually, if we use -coverpkg=./..., many tests cover the SAME target package.
	// We want the coverage OF the target package.
	// So if "in X" is present, use X. If not, use test package Y.

	// Regex for "coverage: X% of statements in PACKAGE"
	reWithPkg := regexp.MustCompile(`coverage:\s+(\d+(\.\d+)?)%\s+of\s+statements\s+in\s+(\S+)`)

	// Regex for simple "coverage: X%" (fallback)
	reSimple := regexp.MustCompile(`coverage:\s+(\d+(\.\d+)?)%`)

	for _, line := range lines {
		if strings.Contains(line, "[no test files]") {
			continue
		}

		// Try explicit target package first
		matchesPkg := reWithPkg.FindStringSubmatch(line)
		if len(matchesPkg) > 3 {
			val, _ := strconv.ParseFloat(matchesPkg[1], 64)
			pkg := matchesPkg[3]
			if val > pkgCoverage[pkg] {
				pkgCoverage[pkg] = val
			}
			continue
		}

		// Fallback to simple coverage (usually implies covering itself)
		// We need to find the package name from the "ok" line start if possible
		// Line format: "ok  package_name  time  coverage: ..."
		matchesSimple := reSimple.FindStringSubmatch(line)
		if len(matchesSimple) > 1 {
			val, _ := strconv.ParseFloat(matchesSimple[1], 64)

			// Try to extract package name from start of line
			fields := strings.Fields(line)
			pkg := ""
			if len(fields) >= 2 && fields[0] == "ok" {
				pkg = fields[1]
			} else {
				// If we can't determine package, use the line itself as unique key to avoid merging
				pkg = line
			}

			if val > pkgCoverage[pkg] {
				pkgCoverage[pkg] = val
			}
		}
	}

	if len(pkgCoverage) == 0 {
		return "0"
	}

	var total float64
	for _, val := range pkgCoverage {
		total += val
	}

	return fmt.Sprintf("%.1f", total/float64(len(pkgCoverage)))
}

// exactCoverageFromProfile reads a coverage profile and returns the total percentage.
func exactCoverageFromProfile(profilePath string) string {
	out, err := command.Exec("go", "tool", "cover", fmt.Sprintf("-func=%s", profilePath)).CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "total:") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				return strings.TrimSuffix(parts[len(parts)-1], "%")
			}
		}
	}
	return ""
}

func (g *Go) installWasmBrowserTest() error {
	if _, err := command.RunInDir(g.rootDir, "which", "wasmbrowsertest"); err == nil {
		return nil
	}

	_, err := command.RunInDir(g.rootDir, "go", "install", "webtyp.com/wasmbrowsertest@latest")
	if err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	return nil
}

// wasmTestPackages lists the packages whose tests can actually be built for js/wasm.
//
// It exists because `go test ./...` under GOOS=js is wrong for any repo with two build
// targets (host tooling + an edge binary). A package whose sources are all `//go:build
// !wasm` still gets compiled by `./...`, and fails with "build constraints exclude all
// Go files" — a red suite that reports nothing about the code. The same goes for any
// package importing one. Selecting the packages up front means the runner only builds
// what the wasm target actually contains.
//
// Falls back to "./..." if go list gives nothing, so the caller still sees a real error
// instead of an empty, silently-passing run.
func (g *Go) wasmTestPackages(runAll bool) []string {
	args := []string{"list", "-f", "{{.ImportPath}} {{len .GoFiles}} {{len .TestGoFiles}} {{len .XTestGoFiles}}"}
	if runAll {
		args = append(args, "-tags=integration")
	}
	args = append(args, "./...")

	cmd := exec.Command("go", args...)
	cmd.Dir = g.rootDir
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, _ := cmd.Output() // stderr carries the excluded packages: expected, not fatal

	pkgs := ParseWasmTestPackages(string(out))
	if len(pkgs) == 0 {
		return []string{"./..."}
	}
	return pkgs
}

// ParseWasmTestPackages keeps the packages that have tests AND can be built for wasm.
func ParseWasmTestPackages(goListOut string) []string {
	var pkgs []string
	for _, line := range strings.Split(goListOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		path := fields[0]
		goFiles, err1 := strconv.Atoi(fields[1])
		testFiles, err2 := strconv.Atoi(fields[2])
		xTestFiles, err3 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}

		if testFiles == 0 && xTestFiles == 0 {
			continue // nothing to run here
		}
		// An internal test needs the package's own sources. With none of them left
		// under wasm, the package cannot compile: that is a host-only package, not
		// a failure.
		if testFiles > 0 && goFiles == 0 {
			continue
		}
		pkgs = append(pkgs, path)
	}
	return pkgs
}

// ShouldEnableWasm decides if WASM tests should be run based on go list output differences
func ShouldEnableWasm(nativeOut, wasmOut string) bool {
	// fmt.Printf("DEBUG: ShouldEnableWasm check starting\n")
	nativeFiles := parseGoListFiles(nativeOut)
	// fmt.Printf("DEBUG: ShouldEnableWasm - Native files found: %d\n", len(nativeFiles))
	wasmFiles := parseGoListFiles(wasmOut)
	// fmt.Printf("DEBUG: ShouldEnableWasm - WASM files found: %d\n", len(wasmFiles))

	// Activation condition: at least one test file in WASM that is NOT in Native
	// This means it has a //go:build wasm tag or similar.
	for f := range wasmFiles {
		if !nativeFiles[f] {
			return true
		}
	}
	return false
}

// HasVFlag checks if -v is already present in the args
func HasVFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || arg == "-test.v" {
			return true
		}
	}
	return false
}

// parseGoListFiles converts the output of go list into a map of unique test files
func parseGoListFiles(output string) map[string]bool {
	fileMap := make(map[string]bool)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// fmt.Printf("DEBUG: parse line: %q\n", line)
		// Legitimate go list lines for this template usually contain '['
		// but we must skip error messages that might start with "package" or involve "syscall/js"
		if !strings.Contains(line, "[") {
			continue
		}

		// Extract package path and file list: "path [a_test.go b_test.go] []"
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		pkgPath := parts[0]
		fileList := parts[1]

		// Final check: pkgPath shouldn't have spaces if it's a real path from go list
		if strings.Contains(pkgPath, " ") {
			continue
		}

		// Normalize file list and add to map: pkgPath/file
		fileList = strings.ReplaceAll(fileList, "[", "")
		fileList = strings.ReplaceAll(fileList, "]", "")
		files := strings.Fields(fileList)
		for _, f := range files {
			fileMap[pkgPath+"/"+f] = true
		}
	}
	// fmt.Printf("DEBUG: Found %d unique test files\n", len(fileMap))
	return fileMap
}

// EvaluateTestResults analyzes the output of go test and decides the outcome
// This function is pure and can be easily tested.
func EvaluateTestResults(err error, output, moduleName string, msgs []string, skipRace bool) (testStatus, raceStatus string, stdTestsRan bool, newMsgs []string) {
	testStatus = "Failed"
	raceStatus = "Detected"
	if skipRace {
		raceStatus = "Skipped"
	}

	newMsgs = msgs

	addMsg := func(ok bool, msg string) {
		symbol := "✅"
		if !ok {
			symbol = "❌"
		}
		newMsgs = append(newMsgs, fmt.Sprintf("%s %s", msg, symbol))
	}

	// Determine if any stdlib tests actually ran by looking for ok/FAIL markers in output
	// Use more robust matching that handles different spacing/tabs
	hasStdOk := strings.Contains(output, "ok  ") || strings.Contains(output, "ok\t") || strings.Contains(output, "\tok\t")
	hasStdFail := strings.Contains(output, "FAIL  ") || strings.Contains(output, "FAIL\t") || strings.Contains(output, "\tFAIL\t")
	stdTestsRan = hasStdOk || hasStdFail

	if err == nil {
		testStatus = "Passing"
		if !skipRace {
			raceStatus = "Clean"
			addMsg(true, "race")
		} else {
			addMsg(true, "race skipped")
		}
		addMsg(true, "tests")
		stdTestsRan = true
		return
	}

	// It failed (exit code != 0). Is it a real test failure or just build constraints?
	// Check for real test failures: "--- FAIL"
	// Also check for "FAIL\t" but EXCLUDE "[setup failed]" if we have valid tests passing elsewhere
	hasRealFailures := strings.Contains(output, "--- FAIL")

	if !hasRealFailures {
		// Look for FAIL lines that are NOT setup failures
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if (strings.Contains(line, "FAIL\t") || strings.Contains(line, "FAIL  ")) &&
				!strings.Contains(line, "[setup failed]") {
				hasRealFailures = true
				break
			}
		}
	}

	// Check for build failures: "[build failed]" or similar
	hasBuildFailures := strings.Contains(output, "[build failed]")

	// Check for exclusion errors (can be explicit or part of setup failed)
	isExclusionError := strings.Contains(output, "matched no packages") ||
		strings.Contains(output, "build constraints exclude all Go files")

	// Special case: Setup failed due to build constraints but other tests PASSED
	if !hasRealFailures && !hasBuildFailures {
		if strings.Contains(output, "[setup failed]") && isExclusionError && hasStdOk {
			// This is the "Partial Success" scenario (client)
			// Treat as success
		} else if strings.Contains(output, "[setup failed]") {
			// Setup failed for other reasons (and no other success confirmed logic override)
			hasRealFailures = true
		}
	}

	if !hasRealFailures && !hasBuildFailures && (isExclusionError || hasStdOk) {
		// It's a "Partial Success" or "Exclusion Only"
		testStatus = "Passing"
		if !skipRace {
			raceStatus = "Clean"
			if stdTestsRan {
				addMsg(true, "race")
			}
		} else {
			if stdTestsRan {
				addMsg(true, "race skipped")
			}
		}

		if stdTestsRan {
			addMsg(true, "tests")
		}
	} else {
		// Real failure
		addMsg(false, fmt.Sprintf("Test errors found in %s", moduleName))
	}

	return
}

// HasTimeoutFlag checks if -timeout is already present in the args
func HasTimeoutFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-timeout" || strings.HasPrefix(arg, "-timeout=") ||
			arg == "-test.timeout" || strings.HasPrefix(arg, "-test.timeout=") {
			return true
		}
	}
	return false
}

// FindTimedOutTests parses go test output and extracts test names that timed out.
// Handles two scenarios:
// 1. Go's native timeout: "panic: test timed out after Ns\n  running tests:\n    TestName (Ns)"
// 2. Process killed externally (context.WithTimeout): finds the last "=== RUN" without a matching "--- PASS/FAIL"
func FindTimedOutTests(output string) []string {
	// Try Go's native timeout format: "running tests:" section
	if strings.Contains(output, "running tests:") {
		re := regexp.MustCompile(`(?m)^\s+(\S+)\s+\(\d+`)
		var names []string
		inRunning := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "running tests:") {
				inRunning = true
				continue
			}
			if inRunning {
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					names = append(names, matches[1])
				} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "goroutine") {
					continue
				} else {
					break
				}
			}
		}
		if len(names) > 0 {
			return names
		}
	}

	// Fallback: find the last "=== RUN" without a matching "--- PASS/FAIL"
	// Works when process is killed externally (context timeout, SIGKILL)
	var lastRun string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "=== RUN") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				lastRun = fields[2]
			}
		}
		if strings.Contains(line, "--- PASS:") || strings.Contains(line, "--- FAIL:") || strings.Contains(line, "--- SKIP:") {
			lastRun = ""
		}
	}
	if lastRun != "" {
		return []string{lastRun}
	}

	return nil
}

// discoverWasmTestNames scans WASM test source files for func TestXxx declarations.
// Used as fallback when wasmbrowsertest doesn't relay === RUN lines before a timeout kill.
// findWasmTimeoutCulprit retries WASM tests individually to identify which test hangs.
// Called after a bulk WASM run times out (wasmbrowsertest buffers output, so we can't
// determine the culprit from the output buffer).
func (g *Go) findWasmTimeoutCulprit(timeoutSec int) []string {
	names := g.discoverWasmTestNames()
	if len(names) <= 1 {
		return names
	}

	g.log("Identifying timed out wasm test...")

	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		cmd := GoTestCmdFn(ctx, g.rootDir, "go", "test", "-exec", "wasmbrowsertest", "-run", "^"+name+"$", "-v", "./...")
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Run()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut {
			return []string{name}
		}
	}
	return nil
}

func (g *Go) discoverWasmTestNames() []string {
	listCmd := exec.Command("go", "list", "-f",
		`{{range .TestGoFiles}}{{$.Dir}}/{{.}} {{end}}{{range .XTestGoFiles}}{{$.Dir}}/{{.}} {{end}}`,
		"./...")
	listCmd.Dir = g.rootDir
	listCmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := listCmd.Output()
	if err != nil {
		return nil
	}

	re := regexp.MustCompile(`func (Test\w+)\(`)
	var names []string
	seen := make(map[string]bool)
	for _, path := range strings.Fields(string(out)) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			if !seen[m[1]] {
				names = append(names, m[1])
				seen[m[1]] = true
			}
		}
	}
	return names
}
