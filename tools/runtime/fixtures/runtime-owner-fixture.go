package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"

	proxylensruntime "github.com/Whatnamed/ProxyLens/collector/pkg/runtime"
)

// runtime-owner-fixture is a no-network Runtime stand-in for the isolated
// Task Scheduler background-host regression. It owns the same per-DB mutex and
// speaks only the narrow Runtime READY/STOP protocol; it never opens SQLite or
// contacts a Controller.
func main() {
	dbPath := flag.String("db", "", "isolated authority DB identity")
	flag.Parse()
	if *dbPath == "" {
		os.Exit(20)
	}
	owner, err := proxylensruntime.AcquireRuntimeOwnership(*dbPath)
	if err != nil {
		os.Exit(21)
	}
	defer owner.Close()
	if err := proxylensruntime.EncodeRuntimeReady(os.Stdout, "background-host-fixture", os.Getpid()); err != nil {
		os.Exit(22)
	}

	stop := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if scanner.Text() == "STOP" {
				close(stop)
				return
			}
		}
		// An abrupt Supervisor exit closes the pipe. Keep the fixture alive so
		// the next periodic host observes the external Runtime owner exactly as
		// production Supervisor recovery does.
	}()
	select {
	case <-stop:
	case <-time.After(24 * time.Hour):
		fmt.Fprintln(os.Stderr, "fixture lifetime expired")
	}
}
