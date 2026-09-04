package runtime

import (
	"fmt"
	"testing"
)

func testControllerURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func TestResolveControllerURLPrecedence(t *testing.T) {
	const mockURL = "http://127.0.0.1:43127"
	for _, tc := range []struct {
		name   string
		cli    string
		env    string
		def    string
		want   string
		source ControllerURLSource
	}{
		{name: "cli", cli: mockURL, env: "http://127.0.0.1:43128", def: "http://127.0.0.1:43129", want: mockURL, source: ControllerURLSourceCLI},
		{name: "environment", env: mockURL, def: "http://127.0.0.1:43129", want: mockURL, source: ControllerURLSourceEnv},
		{name: "default", def: mockURL, want: mockURL, source: ControllerURLSourceDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, source, err := ResolveControllerURL(tc.cli, tc.env, tc.def)
			if err != nil {
				t.Fatalf("ResolveControllerURL failed: %v", err)
			}
			if got != tc.want || source != tc.source {
				t.Fatalf("got URL=%q source=%q, want URL=%q source=%q", got, source, tc.want, tc.source)
			}
		})
	}
}

func TestResolveControllerURLRejectsAllBlankValues(t *testing.T) {
	if _, _, err := ResolveControllerURL(" \t", "", ""); err == nil {
		t.Fatal("expected blank controller URL resolution to fail")
	}
}

func TestResolveControllerURLForModeKeepsProductionDefault(t *testing.T) {
	productDefault := testControllerURL(9090)

	got, source, err := ResolveControllerURLForMode("", "", productDefault, false)
	if err != nil {
		t.Fatalf("production controller resolution failed: %v", err)
	}
	if got != productDefault || source != ControllerURLSourceDefault {
		t.Fatalf("got URL=%q source=%q, want URL=%q source=%q", got, source, productDefault, ControllerURLSourceDefault)
	}
}

func TestResolveControllerURLForModeE2EAcceptsExplicitRandomLoopbackMock(t *testing.T) {
	const mockURL = "http://127.0.0.1:43127"

	for _, tc := range []struct {
		name   string
		cli    string
		env    string
		source ControllerURLSource
	}{
		{name: "cli", cli: mockURL, env: "http://127.0.0.1:43128", source: ControllerURLSourceCLI},
		{name: "environment", env: mockURL, source: ControllerURLSourceEnv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, source, err := ResolveControllerURLForMode(tc.cli, tc.env, testControllerURL(9090), true)
			if err != nil {
				t.Fatalf("E2E controller resolution failed: %v", err)
			}
			if got != mockURL || source != tc.source {
				t.Fatalf("got URL=%q source=%q, want URL=%q source=%q", got, source, mockURL, tc.source)
			}
		})
	}
}

func TestResolveControllerURLForModeE2ERejectsMissingOverrideAndProductDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		def  string
	}{
		{name: "missing override", def: ""},
		{name: "product default", def: testControllerURL(9090)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ResolveControllerURLForMode("", "", tc.def, true); err == nil {
				t.Fatal("expected E2E controller resolution to fail closed")
			}
		})
	}
}

func TestValidateE2EControllerURLRejectsUnsafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "9090", url: testControllerURL(9090)},
		{name: "7988", url: testControllerURL(7988)},
		{name: "malformed", url: "not-a-url"},
		{name: "missing port", url: "http://127.0.0.1"},
		{name: "zero port", url: testControllerURL(0)},
		{name: "non-loopback", url: "http://192.0.2.1:43127"},
		{name: "hostname loopback alias", url: "http://localhost:43127"},
		{name: "non-http", url: "https://127.0.0.1:43127"},
		{name: "query", url: "http://127.0.0.1:43127?mock=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateE2EControllerURL(tc.url); err == nil {
				t.Fatalf("expected unsafe E2E Controller URL %q to fail", tc.url)
			}
		})
	}
}

func TestNewCollectorRunnerKeepsInheritedSecretInMemory(t *testing.T) {
	const syntheticSecret = "phase3e2a-test-secret"
	t.Setenv("MIHOMO_SECRET", syntheticSecret)

	runner, err := NewCollectorRunner(CollectorOptions{ControllerURL: "http://mock.invalid"})
	if err != nil {
		t.Fatalf("NewCollectorRunner failed: %v", err)
	}
	if got := runner.Config().Secret; got != syntheticSecret {
		t.Fatal("expected inherited secret to reach runner config")
	}
}
