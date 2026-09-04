package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	RuntimeReadySignalType             = "proxylens-runtime-ready"
	RuntimeAlreadyRunningSignalType    = "proxylens-runtime-already-running"
	SupervisorReadySignalType          = "proxylens-supervisor-ready"
	SupervisorAlreadyRunningSignalType = "proxylens-supervisor-already-running"
	SupervisorRuntimeRestartedType     = "proxylens-supervisor-runtime-restarted"
)

type RuntimeSignal struct {
	Type           string
	RuntimeVersion string
	PID            int
}

type runtimeSignalWire struct {
	Type           string `json:"type"`
	RuntimeVersion string `json:"runtimeVersion"`
	PID            *int   `json:"pid,omitempty"`
}

// ParseRuntimeSignal parses only the two exact Runtime machine signals. Logs,
// malformed JSON, unknown fields, and secret-bearing payloads are ignored.
func ParseRuntimeSignal(line string) (RuntimeSignal, bool) {
	var wire runtimeSignalWire
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil || strings.TrimSpace(wire.RuntimeVersion) == "" {
		return RuntimeSignal{}, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RuntimeSignal{}, false
	}
	switch wire.Type {
	case RuntimeReadySignalType:
		if wire.PID == nil || *wire.PID <= 0 {
			return RuntimeSignal{}, false
		}
		return RuntimeSignal{Type: wire.Type, RuntimeVersion: wire.RuntimeVersion, PID: *wire.PID}, true
	case RuntimeAlreadyRunningSignalType:
		if wire.PID != nil {
			return RuntimeSignal{}, false
		}
		return RuntimeSignal{Type: wire.Type, RuntimeVersion: wire.RuntimeVersion}, true
	default:
		return RuntimeSignal{}, false
	}
}

func EncodeRuntimeReady(writer io.Writer, runtimeVersion string, pid int) error {
	if runtimeVersion == "" || pid <= 0 {
		return fmt.Errorf("runtime READY requires a non-empty version and positive PID")
	}
	return json.NewEncoder(writer).Encode(runtimeSignalWire{
		Type:           RuntimeReadySignalType,
		RuntimeVersion: runtimeVersion,
		PID:            &pid,
	})
}

func EncodeRuntimeAlreadyRunning(writer io.Writer, runtimeVersion string) error {
	if runtimeVersion == "" {
		return fmt.Errorf("runtime ALREADY_RUNNING requires a non-empty version")
	}
	return json.NewEncoder(writer).Encode(runtimeSignalWire{
		Type:           RuntimeAlreadyRunningSignalType,
		RuntimeVersion: runtimeVersion,
	})
}

type SupervisorRuntimeState string

const (
	SupervisorRuntimeStateStarted        SupervisorRuntimeState = "started"
	SupervisorRuntimeStateAlreadyRunning SupervisorRuntimeState = "already-running"
	SupervisorRuntimeStateStarting       SupervisorRuntimeState = "starting-retrying"
)

type SupervisorReadyInfo struct {
	RuntimeState SupervisorRuntimeState
	RuntimePID   int
}

type SupervisorRuntimeRestartInfo struct {
	RuntimePID   int
	RestartCount int
}

type supervisorReadySignalWire struct {
	Type              string                 `json:"type"`
	SupervisorVersion string                 `json:"supervisorVersion"`
	PID               int                    `json:"pid"`
	RuntimeState      SupervisorRuntimeState `json:"runtimeState"`
	RuntimePID        int                    `json:"runtimePid,omitempty"`
}

type supervisorRuntimeRestartedWire struct {
	Type              string `json:"type"`
	SupervisorVersion string `json:"supervisorVersion"`
	PID               int    `json:"pid"`
	RuntimePID        int    `json:"runtimePid"`
	RestartCount      int    `json:"restartCount"`
}

type supervisorAlreadyRunningSignalWire struct {
	Type              string `json:"type"`
	SupervisorVersion string `json:"supervisorVersion"`
}

func EncodeSupervisorReady(writer io.Writer, supervisorVersion string, pid int, info SupervisorReadyInfo) error {
	if supervisorVersion == "" || pid <= 0 {
		return fmt.Errorf("supervisor READY requires a non-empty version and positive PID")
	}
	if info.RuntimeState != SupervisorRuntimeStateStarted && info.RuntimeState != SupervisorRuntimeStateAlreadyRunning && info.RuntimeState != SupervisorRuntimeStateStarting {
		return fmt.Errorf("supervisor READY requires a supported Runtime state")
	}
	switch info.RuntimeState {
	case SupervisorRuntimeStateStarted:
		if info.RuntimePID <= 0 {
			return fmt.Errorf("supervisor READY with started Runtime requires a positive Runtime PID")
		}
	case SupervisorRuntimeStateAlreadyRunning, SupervisorRuntimeStateStarting:
		if info.RuntimePID != 0 {
			return fmt.Errorf("supervisor READY with %s Runtime must not include a Runtime PID", info.RuntimeState)
		}
	}
	return json.NewEncoder(writer).Encode(supervisorReadySignalWire{
		Type:              SupervisorReadySignalType,
		SupervisorVersion: supervisorVersion,
		PID:               pid,
		RuntimeState:      info.RuntimeState,
		RuntimePID:        info.RuntimePID,
	})
}

func EncodeSupervisorAlreadyRunning(writer io.Writer, supervisorVersion string) error {
	if supervisorVersion == "" {
		return fmt.Errorf("supervisor ALREADY_RUNNING requires a non-empty version")
	}
	return json.NewEncoder(writer).Encode(supervisorAlreadyRunningSignalWire{
		Type:              SupervisorAlreadyRunningSignalType,
		SupervisorVersion: supervisorVersion,
	})
}

func EncodeSupervisorRuntimeRestarted(writer io.Writer, supervisorVersion string, pid int, info SupervisorRuntimeRestartInfo) error {
	if supervisorVersion == "" || pid <= 0 || info.RuntimePID <= 0 || info.RestartCount <= 0 {
		return fmt.Errorf("supervisor Runtime restart signal has invalid identity")
	}
	return json.NewEncoder(writer).Encode(supervisorRuntimeRestartedWire{
		Type:              SupervisorRuntimeRestartedType,
		SupervisorVersion: supervisorVersion,
		PID:               pid,
		RuntimePID:        info.RuntimePID,
		RestartCount:      info.RestartCount,
	})
}

// ListenForExactStop cancels only on a line whose content is exactly STOP.
// EOF and all other lines are deliberately ignored so closing a parent pipe
// cannot reintroduce UI-close shutdown semantics.
func ListenForExactStop(reader io.Reader, cancel func()) {
	if reader == nil || cancel == nil {
		return
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if scanner.Text() == "STOP" {
			cancel()
			return
		}
	}
}
