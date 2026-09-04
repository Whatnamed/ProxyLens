package runtime

import "testing"

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

func TestNewCollectorRunnerKeepsInheritedSecretInMemory(t *testing.T) {
	const syntheticSecret = "phase3e2a-test-secret"
	t.Setenv("MIHOMO_SECRET", syntheticSecret)

	runner, err := NewCollectorRunner(CollectorOptions{ControllerURL: "http://mock.invalid"})
	if err != nil {
		t.Fatalf("NewCollectorRunner failed: %v", err)
	}
	if got := runner.Config().Secret; got != syntheticSecret {
		t.Fatalf("expected inherited secret to reach runner config, got %q", got)
	}
}
