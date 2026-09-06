package devflow_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"webtyp.com/devflow"
)

func TestWatchdogCumulativeNotKilled(t *testing.T) {
	// A stream of many fast tests whose total exceeds the limit does NOT trigger a kill
	timeout := 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	var mu sync.Mutex
	killed := false
	onKill := func() {
		mu.Lock()
		killed = true
		mu.Unlock()
		cancel()
	}

	w := devflow.NewWatchdog(timeout, onKill)
	w.Start()
	defer w.Stop()

	// Simulate 5 tests, each taking 30ms. Total 150ms > 100ms timeout.
	// But no individual test exceeds 100ms.
	for i := 1; i <= 5; i++ {
		w.Add(string([]byte("=== RUN   TestFast\n")))
		time.Sleep(30 * time.Millisecond)
		w.Add(string([]byte("--- PASS: TestFast (0.03s)\n")))
		mu.Lock()
		isKilled := killed
		mu.Unlock()
		if isKilled {
			t.Fatalf("Watchdog killed process prematurely at test %d", i)
		}
	}

	mu.Lock()
	isKilled := killed
	mu.Unlock()
	if isKilled {
		t.Error("Watchdog should not have killed the process")
	}
}

func TestWatchdogStallKilled(t *testing.T) {
	// A single test with no completion event within the limit DOES trigger
	timeout := 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	var mu sync.Mutex
	killed := false
	onKill := func() {
		mu.Lock()
		killed = true
		mu.Unlock()
		cancel()
	}

	w := devflow.NewWatchdog(timeout, onKill)
	w.Start()
	defer w.Stop()

	w.Add("=== RUN   TestStall\n")

	// Wait for watchdog to fire
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	isKilled := killed
	mu.Unlock()
	if !isKilled {
		t.Error("Watchdog should have killed the stalled process")
	}

	culprits := w.Culprits()
	if len(culprits) != 1 || culprits[0] != "TestStall" {
		t.Errorf("Expected culprit TestStall, got %v", culprits)
	}
}

func TestWatchdogPauseCont(t *testing.T) {
	// PAUSE/CONT sequences do not blame paused tests
	timeout := 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	var mu sync.Mutex
	killed := false
	onKill := func() {
		mu.Lock()
		killed = true
		mu.Unlock()
		cancel()
	}

	w := devflow.NewWatchdog(timeout, onKill)
	w.Start()
	defer w.Stop()

	w.Add("=== RUN   TestParallel\n")
	w.Add("=== PAUSE TestParallel\n")

	// Stay paused for longer than timeout
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	isKilled := killed
	mu.Unlock()
	if isKilled {
		t.Fatal("Watchdog should not kill a paused test")
	}

	w.Add("=== CONT  TestParallel\n")

	// Now it should be active. Wait a bit less than timeout.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	isKilled = killed
	mu.Unlock()
	if isKilled {
		t.Fatal("Watchdog killed test too early after CONT")
	}

	// Now wait to exceed timeout
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	isKilled = killed
	mu.Unlock()
	if !isKilled {
		t.Error("Watchdog should have killed test after it exceeded timeout post-CONT")
	}
}

func TestWatchdogFragmentedLines(t *testing.T) {
	timeout := 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	var mu sync.Mutex
	killed := false
	onKill := func() {
		mu.Lock()
		killed = true
		mu.Unlock()
		cancel()
	}

	w := devflow.NewWatchdog(timeout, onKill)
	w.Start()
	defer w.Stop()

	// Fragmented output
	w.Add("=== RUN ")
	w.Add("  TestFrag\n")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	isKilled := killed
	mu.Unlock()
	if !isKilled {
		t.Error("Watchdog should have detected fragmented RUN line")
	}

	culprits := w.Culprits()
	if len(culprits) != 1 || culprits[0] != "TestFrag" {
		t.Errorf("Expected culprit TestFrag, got %v", culprits)
	}
}
