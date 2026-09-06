package main

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"strconv"

	"webtyp.com/devflow"
)

func main() {
	usage := func() {
		fmt.Println("Usage: gotest [-t seconds] [-no-cache] [go test flags]")
		fmt.Println()
		fmt.Println("No args: Full test suite (vet, race, cover, wasm, badges)")
		fmt.Println("With args: Pass flags to 'go test' (no vet/wasm/badges/cache)")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -t N        Per-package timeout in seconds (default: 30)")
		fmt.Println("  -no-cache   Force re-execution of tests, skipping cache")
		fmt.Println("  -all        Run all tests including integration tests (sets timeout to 60s)")
		fmt.Println("  -tinygo     Compile the WASM suite with TinyGo instead of the Go toolchain")
		fmt.Println("              (slow: TinyGo goes through LLVM. Requires tinygo installed.)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  gotest              # Full suite, 30s timeout")
		fmt.Println("  gotest -all         # Full suite + integration, 60s timeout")
		fmt.Println("  gotest -no-cache    # Force re-run full suite")
		fmt.Println("  gotest -t 120       # Full suite, 120s timeout")
		fmt.Println("  gotest -run TestFoo # Run specific test, 30s timeout")
		fmt.Println("  gotest -bench .     # Run benchmarks")
	}

	_, _, isHelp, _ := devflow.ParseCLIArgs(os.Args)
	if isHelp {
		usage()
		os.Exit(0)
	}

	// Extract flags
	timeoutSec := 0
	noCache := false
	runAll := false
	useTinygo := false
	var customArgs []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-t" && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil && v > 0 {
				timeoutSec = v
			}
			i++ // skip value
		} else if args[i] == "-no-cache" {
			noCache = true
		} else if args[i] == "-all" {
			runAll = true
		} else if args[i] == "-tinygo" {
			useTinygo = true
			noCache = true // a cached Go-toolchain result would mask the TinyGo run
		} else {
			customArgs = append(customArgs, args[i])
		}
	}

	// Set default timeout if not specified
	if timeoutSec == 0 {
		if runAll {
			timeoutSec = 60
		} else {
			timeoutSec = 30
		}
	}

	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	goHandler.UseTinygo(useTinygo)

	summary, err := goHandler.Test(customArgs, false, timeoutSec, noCache, runAll)
	if err != nil {
		fmt.Println("Tests failed:", err)
		os.Exit(1)
	}

	fmt.Println(summary)
}
