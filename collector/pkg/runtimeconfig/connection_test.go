package runtimeconfig

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestResolveRuntimeConnectionControllerPrecedence(t *testing.T) {
	persisted := RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion, ControllerURL: "http://persisted.example.test"}
	connection, err := ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		CLIControllerURL:         "http://cli.example.test",
		EnvironmentControllerURL: "http://env.example.test",
		PersistedConfig:          persisted,
		ProductDefaultURL:        "http://default.example.test",
	})
	if err != nil || connection.ControllerSource != ControllerSourceCLI {
		t.Fatalf("CLI precedence connection=%+v err=%v", connection.Metadata(), err)
	}
	connection, err = ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		EnvironmentControllerURL: "http://env.example.test",
		PersistedConfig:          persisted,
		ProductDefaultURL:        "http://default.example.test",
	})
	if err != nil || connection.ControllerSource != ControllerSourceEnv {
		t.Fatalf("env precedence connection=%+v err=%v", connection.Metadata(), err)
	}
	connection, err = ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		PersistedConfig:   persisted,
		ProductDefaultURL: "http://default.example.test",
	})
	if err != nil || connection.ControllerSource != ControllerSourcePersisted {
		t.Fatalf("persisted precedence connection=%+v err=%v", connection.Metadata(), err)
	}
}

func TestResolveRuntimeConnectionSecretPrecedenceAndSafeMetadata(t *testing.T) {
	store := NewMemorySecretStore()
	if err := store.Write(context.Background(), "credential-secret"); err != nil {
		t.Fatal(err)
	}
	connection, err := ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		ProductDefaultURL: "http://default.example.test",
		EnvironmentSecret: "environment-secret",
		SecretStore:       store,
	})
	if err != nil || connection.Secret != "environment-secret" || connection.SecretSource != SecretSourceEnvironment {
		t.Fatalf("environment secret precedence connection=%+v err=%v", connection.Metadata(), err)
	}
	if strings.Contains(fmt.Sprintf("%+v", connection.Metadata()), "secret-value") {
		t.Fatal("safe metadata contained a secret value")
	}
	connection, err = ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		ProductDefaultURL: "http://default.example.test",
		SecretStore:       store,
	})
	if err != nil || connection.Secret != "credential-secret" || connection.SecretSource != SecretSourceCredential {
		t.Fatalf("credential secret precedence connection=%+v err=%v", connection.Metadata(), err)
	}
	if err := store.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	connection, err = ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		ProductDefaultURL: "http://default.example.test",
		SecretStore:       store,
	})
	if err != nil || connection.SecretPresent || connection.SecretSource != SecretSourceNone {
		t.Fatalf("empty secret resolution connection=%+v err=%v", connection.Metadata(), err)
	}
}

func TestResolveRuntimeConnectionE2EIgnoresPersistedAndProductDefault(t *testing.T) {
	mockURL := fmt.Sprintf("http://127.0.0.1:%d", 43127)
	connection, err := ResolveRuntimeConnection(context.Background(), RuntimeConnectionOptions{
		EnvironmentControllerURL: mockURL,
		PersistedConfig:          RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion, ControllerURL: "http://persisted.example.test"},
		ProductDefaultURL:        "http://default.example.test",
		E2EMode:                  true,
	})
	if err != nil || connection.ControllerURL != mockURL || connection.ControllerSource != ControllerSourceEnv {
		t.Fatalf("E2E explicit mock resolution connection=%+v err=%v", connection.Metadata(), err)
	}
	for _, opts := range []RuntimeConnectionOptions{
		{PersistedConfig: RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion, ControllerURL: mockURL}, ProductDefaultURL: mockURL, E2EMode: true},
		{EnvironmentControllerURL: testControllerURL(9090), E2EMode: true},
	} {
		if _, err := ResolveRuntimeConnection(context.Background(), opts); err == nil {
			t.Fatalf("unsafe E2E connection unexpectedly resolved: %+v", opts)
		}
	}
}

func TestResolveCredentialTargetIsE2EOnly(t *testing.T) {
	target, err := NewTestCredentialTarget()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTestCredentialTarget(target); err != nil {
		t.Fatalf("generated test target rejected: %v", err)
	}
	if got, err := ResolveCredentialTarget(true, target); err != nil || got != target {
		t.Fatalf("E2E target resolution got=%q err=%v", got, err)
	}
	if got, err := ResolveCredentialTarget(true, "ProxyLens/MihomoController/v1"); err == nil || got != "" {
		t.Fatalf("production target was accepted in E2E: got=%q err=%v", got, err)
	}
	if got, err := ResolveCredentialTarget(false, target); err != nil || got != ProductionCredentialTarget {
		t.Fatalf("production mode did not keep fixed target: got=%q err=%v", got, err)
	}
}
