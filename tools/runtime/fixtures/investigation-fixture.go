package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", "sleep", "fixture mode: exit-zero, exit-nonzero, crash, sleep")
	flag.Parse()

	executable, err := os.Executable()
	if err != nil {
		os.Exit(20)
	}
	fixtureDir := filepath.Dir(executable)
	countPath := filepath.Join(fixtureDir, "investigation-fixture.count")
	pidPath := filepath.Join(fixtureDir, "investigation-fixture.pid")
	logPath := filepath.Join(fixtureDir, "investigation-fixture.log")

	count := 0
	if data, readErr := os.ReadFile(countPath); readErr == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	count++
	_ = os.WriteFile(countPath, []byte(fmt.Sprintf("%d\n", count)), 0o600)
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)

	logEntry := fmt.Sprintf("[%s] PID=%d count=%d mode=%s\n", time.Now().Format(time.RFC3339), os.Getpid(), count, *mode)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(logEntry)
		_ = f.Close()
	}

	switch *mode {
	case "exit-zero":
		os.Exit(0)
	case "exit-nonzero":
		os.Exit(1)
	case "crash":
		// Cause unhandled crash / panic
		panic("intentional fixture crash")
	case "sleep":
		time.Sleep(10 * time.Minute)
	default:
		time.Sleep(10 * time.Minute)
	}
}
