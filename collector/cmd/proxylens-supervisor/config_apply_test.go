package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Whatnamed/ProxyLens/collector/pkg/installedlifecycle"
	"github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"
)

func testApplyConfig() runtimeconfig.RuntimeConfig {
	return runtimeconfig.RuntimeConfig{
		SchemaVersion:    runtimeconfig.RuntimeConfigSchemaVersion,
		ControllerURL:    "http://persisted.example.test",
		AutostartEnabled: true,
	}
}

func testApplyHooks(cfg *runtimeconfig.RuntimeConfig, store runtimeconfig.SecretStore) configApplyHooks {
	return configApplyHooks{
		LoadConfig: func() (runtimeconfig.RuntimeConfig, error) { return *cfg, nil },
		SaveConfig: func(next runtimeconfig.RuntimeConfig) error {
			*cfg = next
			return nil
		},
		ConfigExists: func() bool { return true },
		SecretStore:  store,
	}
}

func applyRequest(url string, autostart bool, action, secret string) configApplyRequest {
	return configApplyRequest{
		ControllerURL:    &url,
		AutostartEnabled: &autostart,
		SecretAction:     action,
		Secret:           secret,
	}
}

func TestApplyRuntimeSettingsKeepDoesNotRestartOrTouchSecret(t *testing.T) {
	cfg := testApplyConfig()
	store := runtimeconfig.NewMemorySecretStore()
	if err := store.Write(context.Background(), "old-secret"); err != nil {
		t.Fatal(err)
	}
	result, err := applyRuntimeSettings(context.Background(), applyRequest(cfg.ControllerURL, true, "keep", ""), testApplyHooks(&cfg, store))
	if err != nil {
		t.Fatalf("keep apply failed: %v", err)
	}
	if !result.Saved || result.RestartRequired || result.SecretChanged || !result.CredentialStored {
		t.Fatalf("unexpected keep result: %+v", result)
	}
	value, found, err := store.Read(context.Background())
	if err != nil || !found || value != "old-secret" {
		t.Fatal("keep apply changed the stored Secret")
	}
}

func TestApplyRuntimeSettingsReplaceAndClearAreExplicitConnectionChanges(t *testing.T) {
	for _, action := range []string{"replace", "clear"} {
		t.Run(action, func(t *testing.T) {
			cfg := testApplyConfig()
			store := runtimeconfig.NewMemorySecretStore()
			if err := store.Write(context.Background(), "old-secret"); err != nil {
				t.Fatal(err)
			}
			secret := ""
			if action == "replace" {
				secret = "new-secret"
			}
			result, err := applyRuntimeSettings(context.Background(), applyRequest(cfg.ControllerURL, true, action, secret), testApplyHooks(&cfg, store))
			if err != nil {
				t.Fatalf("%s apply failed: %v", action, err)
			}
			if !result.Saved || !result.RestartRequired || !result.SecretChanged {
				t.Fatalf("unexpected %s result: %+v", action, result)
			}
			value, found, readErr := store.Read(context.Background())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if action == "replace" && (!found || value != secret) {
				t.Fatal("replace did not persist the new Secret")
			}
			if action == "clear" && found {
				t.Fatal("clear left a stored Secret")
			}
		})
	}
}

func TestApplyRuntimeSettingsRejectsInvalidInputBeforeMutation(t *testing.T) {
	cfg := testApplyConfig()
	store := runtimeconfig.NewMemorySecretStore()
	if err := store.Write(context.Background(), "old-secret"); err != nil {
		t.Fatal(err)
	}
	invalidURL := "http://example.test/path"
	result, err := applyRuntimeSettings(context.Background(), configApplyRequest{
		ControllerURL:    &invalidURL,
		AutostartEnabled: boolPointer(true),
		SecretAction:     "replace",
		Secret:           "new-secret",
	}, testApplyHooks(&cfg, store))
	if !errors.Is(err, errInvalidConfigApplyRequest) || result.Saved {
		t.Fatalf("invalid request was not rejected before mutation: result=%+v err=%v", result, err)
	}
	value, found, readErr := store.Read(context.Background())
	if readErr != nil || !found || value != "old-secret" {
		t.Fatal("invalid request mutated the stored Secret")
	}
}

func TestApplyRuntimeSettingsRollsBackSecretWhenConfigSaveFails(t *testing.T) {
	cfg := testApplyConfig()
	store := runtimeconfig.NewMemorySecretStore()
	if err := store.Write(context.Background(), "old-secret"); err != nil {
		t.Fatal(err)
	}
	hooks := testApplyHooks(&cfg, store)
	hooks.SaveConfig = func(runtimeconfig.RuntimeConfig) error { return fmt.Errorf("synthetic config write failure") }
	result, err := applyRuntimeSettings(context.Background(), applyRequest(cfg.ControllerURL, true, "replace", "new-secret"), hooks)
	if err == nil || result.Saved || result.RollbackIncomplete {
		t.Fatalf("save failure did not safely roll back: result=%+v err=%v", result, err)
	}
	value, found, readErr := store.Read(context.Background())
	if readErr != nil || !found || value != "old-secret" {
		t.Fatal("save failure left the replacement Secret behind")
	}
}

func TestApplyRuntimeSettingsRollsBackConfigSecretAndTaskOnReconcileFailure(t *testing.T) {
	cfg := testApplyConfig()
	store := runtimeconfig.NewMemorySecretStore()
	if err := store.Write(context.Background(), "old-secret"); err != nil {
		t.Fatal(err)
	}
	hooks := testApplyHooks(&cfg, store)
	hooks.TaskMutation = true
	hooks.TaskName = `\ProxyLens-Test\00000000-0000-4000-8000-000000000000`
	hooks.TaskStatus = func() (installedlifecycle.TaskStatus, error) { return installedlifecycle.TaskStatus{}, nil }
	hooks.RegisterTask = func() error { return nil }
	failedOnce := false
	hooks.UnregisterTask = func() error {
		if !failedOnce {
			failedOnce = true
			return fmt.Errorf("synthetic task reconcile failure")
		}
		return nil
	}
	result, err := applyRuntimeSettings(context.Background(), applyRequest(cfg.ControllerURL, false, "replace", "new-secret"), hooks)
	if err == nil || result.Saved || result.RollbackIncomplete {
		t.Fatalf("task failure did not safely roll back: result=%+v err=%v", result, err)
	}
	if cfg != testApplyConfig() {
		t.Fatal("task failure did not restore runtime config")
	}
	value, found, readErr := store.Read(context.Background())
	if readErr != nil || !found || value != "old-secret" {
		t.Fatal("task failure did not restore the previous Secret")
	}
}

func TestDecodeConfigApplyRequestIsStrictAndBounded(t *testing.T) {
	valid := `{"controllerUrl":"http://example.test","autostartEnabled":true,"secretAction":"keep","secret":""}`
	if _, err := decodeConfigApplyRequest(strings.NewReader(valid)); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, input := range []string{
		`{"controllerUrl":"http://example.test","autostartEnabled":true,"secretAction":"keep","secret":"unexpected"}`,
		`{"controllerUrl":"http://example.test","autostartEnabled":true,"secretAction":"clear"} {}`,
		`{"controllerUrl":"http://example.test","autostartEnabled":true,"secretAction":"unknown","secret":""}`,
	} {
		if _, err := decodeConfigApplyRequest(strings.NewReader(input)); err == nil {
			t.Fatalf("unsafe request was accepted: %s", input)
		}
	}
	tooLarge := strings.Repeat("x", configApplyMaxInputBytes+1)
	if _, err := decodeConfigApplyRequest(strings.NewReader(tooLarge)); err == nil {
		t.Fatal("oversized request was accepted")
	}
	encoded, err := json.Marshal(configApplyResponse{configApplyResult: configApplyResult{Saved: true, SecretChanged: true}})
	if err != nil || strings.Contains(string(encoded), "old-secret") {
		t.Fatalf("safe result unexpectedly contains Secret data: %v", err)
	}
}

func boolPointer(value bool) *bool { return &value }
