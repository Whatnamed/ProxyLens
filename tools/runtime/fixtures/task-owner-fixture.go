package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(20)
	}
	fixtureDir := filepath.Dir(executable)
	countPath := filepath.Join(fixtureDir, "task-owner-fixture.count")
	pidPath := filepath.Join(fixtureDir, "task-owner-fixture.pid")
	count := 0
	if data, readErr := os.ReadFile(countPath); readErr == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	count++
	if err := os.WriteFile(countPath, []byte(fmt.Sprintf("%d\n", count)), 0o600); err != nil {
		os.Exit(21)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		os.Exit(22)
	}
	if count == 1 {
		// The isolated harness terminates this known fixture PID to model a
		// crash. Task Scheduler then exercises its RestartOnFailure policy.
		time.Sleep(2 * time.Minute)
		return
	}
	// The harness observes this exact fixture PID before terminating it in
	// finally. It never launches ProxyLens Runtime or touches a controller.
	_ = filepath.Base(executable)
	time.Sleep(2 * time.Minute)
}
