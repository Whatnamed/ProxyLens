package main

import (
	"io"
	"os"

	"github.com/Whatnamed/ProxyLens/collector/pkg/supervisorapp"
)

// The Task Scheduler action is built with the Windows GUI subsystem. It
// shares the Supervisor daemon entry point but deliberately has no terminal
// streams: lifecycle stop is delivered through the exact per-DB stop event.
func main() {
	os.Exit(supervisorapp.Run(os.Args[1:], supervisorapp.Streams{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}))
}
