package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
	"github.com/Whatnamed/ProxyLens/collector/pkg/supervisorapp"
	"golang.org/x/term"
)

type configStatus struct {
	SchemaVersion          int    `json:"schemaVersion"`
	ControllerConfigured   bool   `json:"controllerConfigured"`
	ControllerURL          string `json:"controllerUrl,omitempty"`
	PersistedControllerURL string `json:"persistedControllerUrl"`
	EffectiveControllerURL string `json:"effectiveControllerUrl"`
	ControllerSource       string `json:"controllerSource"`
	AutostartEnabled       bool   `json:"autostartEnabled"`
	CredentialStored       bool   `json:"credentialStored"`
	EffectiveSecretPresent bool   `json:"effectiveSecretPresent"`
	SecretPresent          bool   `json:"secretPresent"`
	SecretSource           string `json:"secretSource"`
	InstalledLayout        bool   `json:"installedLayout"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "config":
			os.Exit(runConfigCommand(os.Args[2:]))
		case "control":
			os.Exit(runControlCommand(os.Args[2:]))
		case "install":
			os.Exit(runInstallCommand(os.Args[2:]))
		}
	}
	os.Exit(supervisorapp.Run(os.Args[1:], supervisorapp.Streams{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}))
}

func runConfigCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: proxylens-supervisor config <status|apply|set-controller|set-autostart|set-secret|clear-secret>")
		return 2
	}
	switch args[0] {
	case "status":
		return configStatusCommand()
	case "apply":
		return configApplyCommand()
	case "set-controller":
		return configSetControllerCommand(args[1:])
	case "set-autostart":
		return configSetAutostartCommand(args[1:])
	case "set-secret":
		return configSetSecretCommand()
	case "clear-secret":
		return configClearSecretCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown config command %q\n", args[0])
		return 2
	}
}

func configStatusCommand() int {
	status, err := buildConfigStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to inspect runtime config: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to emit runtime config status: %v\n", err)
		return 1
	}
	return 0
}

func configSetControllerCommand(args []string) int {
	fs := flag.NewFlagSet("config set-controller", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	controllerURL := fs.String("controller", "", "Controller URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	value := strings.TrimSpace(*controllerURL)
	if os.Getenv(runtimeconfig.E2EModeEnv) == "1" {
		if err := runtimeconfig.ValidateE2EControllerURL(value); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid E2E Controller URL: %v\n", err)
			return 1
		}
	} else if err := runtimeconfig.ValidateControllerURL(value); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid Controller URL: %v\n", err)
		return 1
	}
	cfg, err := loadRuntimeConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load runtime config: %v\n", err)
		return 1
	}
	cfg.ControllerURL = value
	if err := saveRuntimeConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save runtime config: %v\n", err)
		return 1
	}
	return 0
}

func configSetSecretCommand() int {
	store, err := configuredSecretStoreForCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare secure runtime config: %v\n", err)
		return 1
	}
	value, err := readControllerSecret(os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strings.TrimSpace(value) == "" {
		fmt.Fprintln(os.Stderr, "Controller Secret must be non-empty")
		return 1
	}
	if err := store.Write(context.Background(), value); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to persist Controller Secret securely: %v\n", err)
		return 1
	}
	return 0
}

func readControllerSecret(stdin *os.File, prompt io.Writer) (string, error) {
	if stdin == nil {
		return "", fmt.Errorf("Controller Secret stdin is unavailable")
	}
	if term.IsTerminal(int(stdin.Fd())) {
		if prompt != nil {
			_, _ = io.WriteString(prompt, "Controller Secret: ")
		}
		value, err := term.ReadPassword(int(stdin.Fd()))
		if prompt != nil {
			_, _ = io.WriteString(prompt, "\n")
		}
		if err != nil {
			return "", fmt.Errorf("failed to read Controller Secret securely: %w", err)
		}
		return string(value), nil
	}

	// Pipes and lifecycle harnesses retain the existing one-line contract.
	reader := bufio.NewScanner(stdin)
	if !reader.Scan() {
		if err := reader.Err(); err != nil {
			return "", fmt.Errorf("failed to read Controller Secret from stdin")
		}
		return "", fmt.Errorf("Controller Secret must be provided as one non-empty stdin line")
	}
	return reader.Text(), nil
}

func configClearSecretCommand() int {
	store, err := configuredSecretStoreForCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prepare secure runtime config: %v\n", err)
		return 1
	}
	if err := store.Delete(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear Controller Secret securely: %v\n", err)
		return 1
	}
	return 0
}

func loadRuntimeConfig() (runtimeconfig.RuntimeConfig, error) {
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		return runtimeconfig.RuntimeConfig{}, err
	}
	return runtimeconfig.LoadConfig(path)
}

func saveRuntimeConfig(cfg runtimeconfig.RuntimeConfig) error {
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		return err
	}
	return runtimeconfig.SaveConfig(path, cfg)
}

func configuredSecretStoreForCommand() (runtimeconfig.SecretStore, error) {
	store, _, err := runtimeconfig.NewConfiguredSecretStore(os.Getenv(runtimeconfig.E2EModeEnv) == "1")
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("Windows Credential Manager target is unavailable")
	}
	return store, nil
}
