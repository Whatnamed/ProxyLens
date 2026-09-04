package runtime

import runtimeconfig "github.com/Whatnamed/ProxyLens/collector/pkg/runtimeconfig"

type ControllerURLSource = runtimeconfig.ControllerSource

const (
	ControllerURLSourceCLI     = runtimeconfig.ControllerSourceCLI
	ControllerURLSourceEnv     = runtimeconfig.ControllerSourceEnv
	ControllerURLSourceDefault = runtimeconfig.ControllerSourceDefault
)

// ResolveControllerURL applies the Runtime CLI contract without making the
// reusable CollectorRunner inherit a process-wide default implicitly.
func ResolveControllerURL(cliValue, envValue, defaultValue string) (string, ControllerURLSource, error) {
	return runtimeconfig.ResolveControllerURL(cliValue, envValue, "", defaultValue, false)
}

// ResolveControllerURLForMode applies the Runtime CLI controller contract for
// both ordinary production startup and isolated E2E startup. Production keeps
// the historical product default; E2E must use an explicit local mock so a
// missing override cannot reach a user's real Mihomo Controller.
func ResolveControllerURLForMode(cliValue, envValue, defaultValue string, e2eMode bool) (string, ControllerURLSource, error) {
	return runtimeconfig.ResolveControllerURL(cliValue, envValue, "", defaultValue, e2eMode)
}

// ValidateE2EControllerURL accepts only the URL shape produced by the local
// mock Controller used by integration/lifecycle tests. The strict shape keeps
// E2E configuration fail-closed and avoids DNS or conventional real-controller
// endpoint ambiguity.
func ValidateE2EControllerURL(rawURL string) error {
	return runtimeconfig.ValidateE2EControllerURL(rawURL)
}
