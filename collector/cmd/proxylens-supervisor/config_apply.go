package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	collectorconfig "github.com/Whatnamed/ProxyLens/collector/pkg/config"
	"github.com/Whatnamed/ProxyLens/collector/pkg/installedlifecycle"
	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
)

const (
	configApplyMaxInputBytes  = 64 * 1024
	configApplyMaxURLBytes    = 2 * 1024
	configApplyMaxSecretBytes = 4 * 1024
)

var (
	errInvalidConfigApplyRequest = errors.New("invalid runtime settings apply request")
	errConfigApplyPersistence    = errors.New("runtime settings could not be saved")
)

type configApplyRequest struct {
	ControllerURL    *string `json:"controllerUrl"`
	AutostartEnabled *bool   `json:"autostartEnabled"`
	SecretAction     string  `json:"secretAction"`
	Secret           string  `json:"secret,omitempty"`
}

type configApplyResult struct {
	Saved              bool `json:"saved"`
	ConnectionChanged  bool `json:"connectionChanged"`
	ControllerChanged  bool `json:"controllerChanged"`
	SecretChanged      bool `json:"secretChanged"`
	AutostartChanged   bool `json:"autostartChanged"`
	RestartRequired    bool `json:"restartRequired"`
	AutostartEnabled   bool `json:"autostartEnabled"`
	CredentialStored   bool `json:"credentialStored"`
	RollbackIncomplete bool `json:"rollbackIncomplete"`
}

type configApplyResponse struct {
	configApplyResult
	Error string `json:"error,omitempty"`
}

type configApplyHooks struct {
	LoadConfig     func() (runtimeconfig.RuntimeConfig, error)
	SaveConfig     func(runtimeconfig.RuntimeConfig) error
	RemoveConfig   func() error
	ConfigExists   func() bool
	SecretStore    runtimeconfig.SecretStore
	TaskMutation   bool
	TaskName       string
	TaskExecutable string
	TaskStatus     func() (installedlifecycle.TaskStatus, error)
	RegisterTask   func() error
	UnregisterTask func() error
}

func configApplyCommand() int {
	request, err := decodeConfigApplyRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	hooks, err := newProductionConfigApplyHooks()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to prepare runtime settings apply")
		return 1
	}
	result, applyErr := applyRuntimeSettings(context.Background(), request, hooks)
	response := configApplyResponse{configApplyResult: result}
	if applyErr != nil {
		response.Error = "persistence-failed"
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to emit runtime settings apply result")
		return 1
	}
	if applyErr != nil {
		return 1
	}
	return 0
}

func decodeConfigApplyRequest(reader io.Reader) (configApplyRequest, error) {
	if reader == nil {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	data, err := io.ReadAll(io.LimitReader(reader, configApplyMaxInputBytes+1))
	if err != nil || len(data) > configApplyMaxInputBytes {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request configApplyRequest
	if err := decoder.Decode(&request); err != nil {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	if request.ControllerURL == nil || request.AutostartEnabled == nil {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	if request.SecretAction != "keep" && request.SecretAction != "replace" && request.SecretAction != "clear" {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	if len([]byte(*request.ControllerURL)) > configApplyMaxURLBytes || len([]byte(request.Secret)) > configApplyMaxSecretBytes {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	if strings.TrimSpace(*request.ControllerURL) != "" {
		if err := runtimeconfig.ValidateControllerURL(*request.ControllerURL); err != nil {
			return configApplyRequest{}, errInvalidConfigApplyRequest
		}
	}
	if request.SecretAction == "replace" && strings.TrimSpace(request.Secret) == "" {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	if request.SecretAction != "replace" && request.Secret != "" {
		return configApplyRequest{}, errInvalidConfigApplyRequest
	}
	return request, nil
}

func newProductionConfigApplyHooks() (configApplyHooks, error) {
	path, err := runtimeconfig.ResolveConfigPathFromEnvironment()
	if err != nil {
		return configApplyHooks{}, err
	}
	store, _, err := runtimeconfig.NewConfiguredSecretStore(os.Getenv(runtimeconfig.E2EModeEnv) == "1")
	if err != nil {
		return configApplyHooks{}, err
	}
	hooks := configApplyHooks{
		LoadConfig: func() (runtimeconfig.RuntimeConfig, error) { return runtimeconfig.LoadConfig(path) },
		SaveConfig: func(cfg runtimeconfig.RuntimeConfig) error { return runtimeconfig.SaveConfig(path, cfg) },
		RemoveConfig: func() error {
			err := os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		},
		ConfigExists: func() bool { _, err := os.Stat(path); return err == nil },
		SecretStore:  store,
	}
	if taskOwnerMutationAllowed() {
		name, err := installedlifecycle.ResolveTaskName()
		if err != nil {
			return configApplyHooks{}, err
		}
		executable, err := currentSupervisorTaskExecutable()
		if err != nil {
			return configApplyHooks{}, err
		}
		hooks.TaskMutation = true
		hooks.TaskName = name
		hooks.TaskExecutable = executable
		hooks.TaskStatus = func() (installedlifecycle.TaskStatus, error) { return installedlifecycle.Status(name) }
		hooks.RegisterTask = func() error { return installedlifecycle.Register(name, executable) }
		hooks.UnregisterTask = func() error { return installedlifecycle.Unregister(name) }
	}
	return hooks, nil
}

func applyRuntimeSettings(ctx context.Context, request configApplyRequest, hooks configApplyHooks) (configApplyResult, error) {
	result := configApplyResult{}
	if request.ControllerURL == nil || request.AutostartEnabled == nil {
		return result, errInvalidConfigApplyRequest
	}
	if request.SecretAction != "keep" && request.SecretAction != "replace" && request.SecretAction != "clear" {
		return result, errInvalidConfigApplyRequest
	}
	if request.SecretAction == "replace" && strings.TrimSpace(request.Secret) == "" {
		return result, errInvalidConfigApplyRequest
	}
	if request.SecretAction != "replace" && request.Secret != "" {
		return result, errInvalidConfigApplyRequest
	}
	controllerURL := strings.TrimSpace(*request.ControllerURL)
	if len([]byte(controllerURL)) > configApplyMaxURLBytes || (controllerURL != "" && runtimeconfig.ValidateControllerURL(controllerURL) != nil) {
		return result, errInvalidConfigApplyRequest
	}
	if len([]byte(request.Secret)) > configApplyMaxSecretBytes {
		return result, errInvalidConfigApplyRequest
	}
	if hooks.LoadConfig == nil || hooks.SaveConfig == nil {
		return result, errConfigApplyPersistence
	}
	oldConfig, err := hooks.LoadConfig()
	if err != nil {
		return result, errConfigApplyPersistence
	}
	newConfig := oldConfig
	newConfig.SchemaVersion = runtimeconfig.RuntimeConfigSchemaVersion
	newConfig.ControllerURL = controllerURL
	newConfig.AutostartEnabled = *request.AutostartEnabled
	result.ControllerChanged = strings.TrimSpace(oldConfig.ControllerURL) != controllerURL
	result.SecretChanged = request.SecretAction != "keep"
	result.AutostartChanged = oldConfig.AutostartEnabled != newConfig.AutostartEnabled
	result.ConnectionChanged = result.ControllerChanged || result.SecretChanged
	result.RestartRequired = result.ConnectionChanged
	result.AutostartEnabled = newConfig.AutostartEnabled

	oldConfigExisted := hooks.ConfigExists == nil || hooks.ConfigExists()
	oldSecret := ""
	oldSecretFound := false
	if result.SecretChanged {
		if hooks.SecretStore == nil {
			return result, errConfigApplyPersistence
		}
		oldSecret, oldSecretFound, err = hooks.SecretStore.Read(ctx)
		if err != nil {
			return result, errConfigApplyPersistence
		}
	}
	var oldTask installedlifecycle.TaskStatus
	if result.AutostartChanged && hooks.TaskMutation {
		if hooks.TaskStatus == nil {
			return result, errConfigApplyPersistence
		}
		oldTask, err = hooks.TaskStatus()
		if err != nil {
			return result, errConfigApplyPersistence
		}
	}

	secretMutated := false
	if result.SecretChanged {
		if request.SecretAction == "replace" {
			err = hooks.SecretStore.Write(ctx, request.Secret)
		} else {
			err = hooks.SecretStore.Delete(ctx)
		}
		if err != nil {
			return result, errConfigApplyPersistence
		}
		secretMutated = true
	}
	if err := hooks.SaveConfig(newConfig); err != nil {
		result.RollbackIncomplete = !restoreSecret(ctx, hooks.SecretStore, oldSecret, oldSecretFound, secretMutated)
		return result, errConfigApplyPersistence
	}

	if result.AutostartChanged && hooks.TaskMutation {
		if newConfig.AutostartEnabled {
			err = hooks.RegisterTask()
		} else {
			err = hooks.UnregisterTask()
		}
		if err != nil {
			result.RollbackIncomplete = rollbackConfigApply(ctx, hooks, oldConfig, oldConfigExisted, oldSecret, oldSecretFound, secretMutated, oldTask)
			return result, errConfigApplyPersistence
		}
	}

	result.Saved = true
	if hooks.SecretStore != nil {
		value, found, readErr := hooks.SecretStore.Read(ctx)
		if readErr != nil {
			result.RollbackIncomplete = rollbackConfigApply(ctx, hooks, oldConfig, oldConfigExisted, oldSecret, oldSecretFound, secretMutated, oldTask)
			return result, errConfigApplyPersistence
		}
		result.CredentialStored = found && strings.TrimSpace(value) != ""
	}
	return result, nil
}

func restoreSecret(ctx context.Context, store runtimeconfig.SecretStore, value string, found, mutated bool) bool {
	if !mutated || store == nil {
		return true
	}
	var err error
	if found {
		err = store.Write(ctx, value)
	} else {
		err = store.Delete(ctx)
	}
	return err == nil
}

func rollbackConfigApply(ctx context.Context, hooks configApplyHooks, oldConfig runtimeconfig.RuntimeConfig, oldConfigExisted bool, oldSecret string, oldSecretFound, secretMutated bool, oldTask installedlifecycle.TaskStatus) bool {
	complete := true
	if hooks.ConfigExists != nil && !oldConfigExisted {
		if hooks.RemoveConfig == nil || hooks.RemoveConfig() != nil {
			complete = false
		}
	} else if hooks.SaveConfig(oldConfig) != nil {
		complete = false
	}
	if !restoreSecret(ctx, hooks.SecretStore, oldSecret, oldSecretFound, secretMutated) {
		complete = false
	}
	if hooks.TaskMutation && (oldTask.Registered || oldTask.Enabled) {
		if hooks.RegisterTask == nil || hooks.RegisterTask() != nil {
			complete = false
		}
	} else if hooks.TaskMutation && hooks.UnregisterTask != nil && hooks.UnregisterTask() != nil {
		complete = false
	}
	return !complete
}

func buildConfigStatus() (configStatus, error) {
	cfg, err := loadRuntimeConfig()
	if err != nil {
		return configStatus{}, err
	}
	e2eMode := os.Getenv(runtimeconfig.E2EModeEnv) == "1"
	defaults := collectorconfig.DefaultConfig()
	connection, err := runtimeconfig.ResolveRuntimeConnection(context.Background(), runtimeconfig.RuntimeConnectionOptions{
		EnvironmentControllerURL: os.Getenv("PROXYLENS_CONTROLLER_URL"),
		PersistedConfig:          cfg,
		ProductDefaultURL:        defaults.ControllerURL,
		E2EMode:                  e2eMode,
		EnvironmentSecret:        os.Getenv("MIHOMO_SECRET"),
		SecretStore:              configuredSecretStoreForStatus(e2eMode),
	})
	if err != nil {
		return configStatus{}, err
	}
	credentialStored, effectiveSecretPresent, secretSource, err := readConfiguredSecretFacts(e2eMode)
	if err != nil {
		return configStatus{}, err
	}
	installedLayout, err := currentInstalledLayout()
	if err != nil {
		return configStatus{}, err
	}
	return configStatus{
		SchemaVersion:          cfg.SchemaVersion,
		ControllerConfigured:   strings.TrimSpace(cfg.ControllerURL) != "",
		ControllerURL:          cfg.ControllerURL,
		PersistedControllerURL: cfg.ControllerURL,
		EffectiveControllerURL: connection.ControllerURL,
		ControllerSource:       string(connection.ControllerSource),
		AutostartEnabled:       cfg.AutostartEnabled,
		CredentialStored:       credentialStored,
		EffectiveSecretPresent: effectiveSecretPresent,
		SecretPresent:          effectiveSecretPresent,
		SecretSource:           secretSource,
		InstalledLayout:        installedLayout,
	}, nil
}

func configuredSecretStoreForStatus(e2eMode bool) runtimeconfig.SecretStore {
	store, _, err := runtimeconfig.NewConfiguredSecretStore(e2eMode)
	if err != nil {
		return nil
	}
	return store
}

func readConfiguredSecretFacts(e2eMode bool) (bool, bool, string, error) {
	store, _, err := runtimeconfig.NewConfiguredSecretStore(e2eMode)
	if err != nil {
		return false, false, string(runtimeconfig.SecretSourceNone), err
	}
	credentialStored := false
	if store != nil {
		value, found, readErr := store.Read(context.Background())
		if readErr != nil {
			return false, false, string(runtimeconfig.SecretSourceNone), readErr
		}
		credentialStored = found && strings.TrimSpace(value) != ""
	}
	if strings.TrimSpace(os.Getenv("MIHOMO_SECRET")) != "" {
		return credentialStored, true, string(runtimeconfig.SecretSourceEnvironment), nil
	}
	if credentialStored {
		return true, true, string(runtimeconfig.SecretSourceCredential), nil
	}
	return false, false, string(runtimeconfig.SecretSourceNone), nil
}
