package runtime

import (
	"fmt"
	"strings"
)

type ControllerURLSource string

const (
	ControllerURLSourceCLI     ControllerURLSource = "CLI"
	ControllerURLSourceEnv     ControllerURLSource = ControllerURLEnv
	ControllerURLSourceDefault ControllerURLSource = "PRODUCT_DEFAULT"
)

// ResolveControllerURL applies the Runtime CLI contract without making the
// reusable CollectorRunner inherit a process-wide default implicitly.
func ResolveControllerURL(cliValue, envValue, defaultValue string) (string, ControllerURLSource, error) {
	if value := strings.TrimSpace(cliValue); value != "" {
		return value, ControllerURLSourceCLI, nil
	}
	if value := strings.TrimSpace(envValue); value != "" {
		return value, ControllerURLSourceEnv, nil
	}
	if value := strings.TrimSpace(defaultValue); value != "" {
		return value, ControllerURLSourceDefault, nil
	}
	return "", "", fmt.Errorf("runtime requires a controller URL from --controller, %s, or the product default", ControllerURLEnv)
}
