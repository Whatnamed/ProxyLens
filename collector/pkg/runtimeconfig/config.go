package runtimeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ConfigDirEnv               = "PROXYLENS_CONFIG_DIR"
	E2EModeEnv                 = "PROXYLENS_E2E_MODE"
	E2ECredentialTargetEnv     = "PROXYLENS_E2E_CREDENTIAL_TARGET"
	E2EStatusFileEnv           = "PROXYLENS_E2E_STATUS_FILE"
	RuntimeConfigFileName      = "runtime.json"
	ProductionCredentialTarget = "ProxyLens/MihomoController/v1"
	TestCredentialTargetPrefix = "ProxyLens/Test/"
	RuntimeConfigSchemaVersion = 1
)

var (
	ErrUnsupportedConfigSchema    = errors.New("unsupported ProxyLens runtime config schema")
	ErrCredentialStoreUnsupported = errors.New("Windows Credential Manager is unsupported on this platform")
)

// RuntimeConfig contains only non-sensitive Runtime configuration. Secrets
// must never be added to this structure or persisted beside it.
type RuntimeConfig struct {
	SchemaVersion int    `json:"schemaVersion"`
	ControllerURL string `json:"controllerUrl"`
}

func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion}
}

// ResolveConfigPath applies the config-specific path contract. It deliberately
// does not read PROXYLENS_DATA_DIR, which remains reserved for the authority DB.
func ResolveConfigPath(configDir, localAppData string) (string, error) {
	if value := strings.TrimSpace(configDir); value != "" {
		return filepath.Clean(filepath.Join(value, RuntimeConfigFileName)), nil
	}
	if value := strings.TrimSpace(localAppData); value != "" {
		return filepath.Clean(filepath.Join(value, "ProxyLens", "config", RuntimeConfigFileName)), nil
	}
	return "", fmt.Errorf("runtime config path cannot be resolved: %s and LOCALAPPDATA are unset", ConfigDirEnv)
}

func ResolveConfigPathFromEnvironment() (string, error) {
	return ResolveConfigPath(os.Getenv(ConfigDirEnv), os.Getenv("LOCALAPPDATA"))
}

// LoadConfig treats a missing file as an unconfigured, valid v1 config. An
// existing malformed or future config fails closed and is never rewritten.
func LoadConfig(path string) (RuntimeConfig, error) {
	if strings.TrimSpace(path) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtime config path must not be blank")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultRuntimeConfig(), nil
	}
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("failed to read runtime config: %w", err)
	}

	var cfg RuntimeConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("malformed runtime config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return RuntimeConfig{}, fmt.Errorf("malformed runtime config: trailing data")
		}
		return RuntimeConfig{}, fmt.Errorf("malformed runtime config: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return RuntimeConfig{}, err
	}
	cfg.ControllerURL = strings.TrimSpace(cfg.ControllerURL)
	return cfg, nil
}

// SaveConfig writes a complete v1 config to a same-directory temporary file,
// flushes it, and atomically replaces the destination. No secret can enter the
// serialized representation because RuntimeConfig has no secret field.
func SaveConfig(path string, cfg RuntimeConfig) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("runtime config path must not be blank")
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	cfg.ControllerURL = strings.TrimSpace(cfg.ControllerURL)

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("failed to create runtime config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode runtime config: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(parent, ".runtime.json-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary runtime config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("failed to protect temporary runtime config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("failed to write temporary runtime config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("failed to flush temporary runtime config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to close temporary runtime config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("failed to atomically replace runtime config: %w", err)
	}
	return nil
}

func validateConfig(cfg RuntimeConfig) error {
	if cfg.SchemaVersion != RuntimeConfigSchemaVersion {
		return fmt.Errorf("%w: got %d, supported %d", ErrUnsupportedConfigSchema, cfg.SchemaVersion, RuntimeConfigSchemaVersion)
	}
	if strings.TrimSpace(cfg.ControllerURL) != "" {
		if err := ValidateControllerURL(cfg.ControllerURL); err != nil {
			return fmt.Errorf("invalid runtime Controller URL: %w", err)
		}
	}
	return nil
}

// ValidateControllerURL accepts a Controller base URL for normal product
// operation. It rejects credentials and URL components that are not part of
// the Controller base contract.
func ValidateControllerURL(rawURL string) error {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return fmt.Errorf("Controller URL must not be blank")
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("Controller URL must be http/https with a host, no userinfo/query/fragment, and an empty base path")
	}
	if portText := u.Port(); portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("Controller URL has an invalid port")
		}
	}
	return nil
}

// ValidateE2EControllerURL is intentionally stricter than the normal product
// validator. It mirrors the local mock shape and refuses conventional Mihomo
// Controller ports so E2E cannot silently contact a user's service.
func ValidateE2EControllerURL(rawURL string) error {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return fmt.Errorf("E2E mode requires a non-empty mock Controller URL")
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("E2E mode requires an http loopback mock Controller URL")
	}
	if u.Hostname() != "127.0.0.1" {
		return fmt.Errorf("E2E mode requires a 127.0.0.1 loopback mock Controller URL")
	}
	portText := u.Port()
	if portText == "" {
		return fmt.Errorf("E2E mode requires an explicit non-zero mock Controller port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("E2E mode requires a valid mock Controller port")
	}
	if port == 9090 || port == 7988 {
		return fmt.Errorf("E2E mode refuses conventional real Controller ports")
	}
	return nil
}
