package runtimeconfig

import (
	"context"
	"fmt"
	"strings"
)

type ControllerSource string

const (
	ControllerSourceCLI       ControllerSource = "CLI"
	ControllerSourceEnv       ControllerSource = "PROXYLENS_CONTROLLER_URL"
	ControllerSourcePersisted ControllerSource = "PERSISTED"
	ControllerSourceDefault   ControllerSource = "PRODUCT_DEFAULT"
)

type SecretSource string

const (
	SecretSourceEnvironment SecretSource = "MIHOMO_SECRET"
	SecretSourceCredential  SecretSource = "CREDENTIAL_MANAGER"
	SecretSourceNone        SecretSource = "NONE"
)

type RuntimeConnectionOptions struct {
	CLIControllerURL         string
	EnvironmentControllerURL string
	PersistedConfig          RuntimeConfig
	ProductDefaultURL        string
	E2EMode                  bool
	EnvironmentSecret        string
	SecretStore              SecretStore
}

type ResolvedRuntimeConnection struct {
	ControllerURL    string
	ControllerSource ControllerSource
	Secret           string
	SecretSource     SecretSource
	SecretPresent    bool
}

type RuntimeConnectionMetadata struct {
	ControllerSource string `json:"controllerSource"`
	SecretSource     string `json:"secretSource"`
	SecretPresent    bool   `json:"secretPresent"`
}

func (c ResolvedRuntimeConnection) Metadata() RuntimeConnectionMetadata {
	return RuntimeConnectionMetadata{
		ControllerSource: string(c.ControllerSource),
		SecretSource:     string(c.SecretSource),
		SecretPresent:    c.SecretPresent,
	}
}

// ResolveControllerURL is the shared controller-only view of the full
// Runtime connection policy. It is kept exported for the existing Runtime
// package compatibility helpers and for focused resolver tests.
func ResolveControllerURL(cliValue, environmentValue, persistedValue, productDefaultValue string, e2eMode bool) (string, ControllerSource, error) {
	return resolveControllerURL(RuntimeConnectionOptions{
		CLIControllerURL:         cliValue,
		EnvironmentControllerURL: environmentValue,
		PersistedConfig:          RuntimeConfig{ControllerURL: persistedValue},
		ProductDefaultURL:        productDefaultValue,
		E2EMode:                  e2eMode,
	})
}

// ResolveRuntimeConnection is the sole Runtime URL/Secret precedence boundary.
// It returns the secret only in memory; callers must use Metadata for safe
// diagnostics and must never serialize or log the ResolvedRuntimeConnection.
func ResolveRuntimeConnection(ctx context.Context, opts RuntimeConnectionOptions) (ResolvedRuntimeConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	controllerURL, controllerSource, err := resolveControllerURL(opts)
	if err != nil {
		return ResolvedRuntimeConnection{}, err
	}

	connection := ResolvedRuntimeConnection{
		ControllerURL:    controllerURL,
		ControllerSource: controllerSource,
		SecretSource:     SecretSourceNone,
	}
	if strings.TrimSpace(opts.EnvironmentSecret) != "" {
		connection.Secret = opts.EnvironmentSecret
		connection.SecretSource = SecretSourceEnvironment
		connection.SecretPresent = true
		return connection, nil
	}
	if opts.SecretStore == nil {
		return connection, nil
	}
	value, found, err := opts.SecretStore.Read(ctx)
	if err != nil {
		return ResolvedRuntimeConnection{}, fmt.Errorf("failed to read Controller Secret from secure storage: %w", err)
	}
	if found && strings.TrimSpace(value) != "" {
		connection.Secret = value
		connection.SecretSource = SecretSourceCredential
		connection.SecretPresent = true
	}
	return connection, nil
}

func resolveControllerURL(opts RuntimeConnectionOptions) (string, ControllerSource, error) {
	if opts.E2EMode {
		if value := strings.TrimSpace(opts.CLIControllerURL); value != "" {
			if err := ValidateE2EControllerURL(value); err != nil {
				return "", "", err
			}
			return value, ControllerSourceCLI, nil
		}
		if value := strings.TrimSpace(opts.EnvironmentControllerURL); value != "" {
			if err := ValidateE2EControllerURL(value); err != nil {
				return "", "", err
			}
			return value, ControllerSourceEnv, nil
		}
		return "", "", fmt.Errorf("E2E mode requires an explicit mock Controller URL from --controller or PROXYLENS_CONTROLLER_URL; persisted and product defaults are disabled")
	}

	candidates := []struct {
		value  string
		source ControllerSource
	}{
		{opts.CLIControllerURL, ControllerSourceCLI},
		{opts.EnvironmentControllerURL, ControllerSourceEnv},
		{opts.PersistedConfig.ControllerURL, ControllerSourcePersisted},
		{opts.ProductDefaultURL, ControllerSourceDefault},
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(candidate.value); value != "" {
			if err := ValidateControllerURL(value); err != nil {
				return "", "", fmt.Errorf("invalid Controller URL from %s: %w", candidate.source, err)
			}
			return value, candidate.source, nil
		}
	}
	return "", "", fmt.Errorf("runtime requires a Controller URL from --controller, PROXYLENS_CONTROLLER_URL, persisted runtime.json, or the product default")
}
