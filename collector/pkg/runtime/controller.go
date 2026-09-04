package runtime

import (
	"fmt"
	"net/url"
	"strconv"
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

// ResolveControllerURLForMode applies the Runtime CLI controller contract for
// both ordinary production startup and isolated E2E startup. Production keeps
// the historical product default; E2E must use an explicit local mock so a
// missing override cannot reach a user's real Mihomo Controller.
func ResolveControllerURLForMode(cliValue, envValue, defaultValue string, e2eMode bool) (string, ControllerURLSource, error) {
	if !e2eMode {
		return ResolveControllerURL(cliValue, envValue, defaultValue)
	}

	if value := strings.TrimSpace(cliValue); value != "" {
		if err := ValidateE2EControllerURL(value); err != nil {
			return "", "", err
		}
		return value, ControllerURLSourceCLI, nil
	}
	if value := strings.TrimSpace(envValue); value != "" {
		if err := ValidateE2EControllerURL(value); err != nil {
			return "", "", err
		}
		return value, ControllerURLSourceEnv, nil
	}

	return "", "", fmt.Errorf("E2E mode requires an explicit mock Controller URL from --controller or %s; PRODUCT_DEFAULT is disabled", ControllerURLEnv)
}

// ValidateE2EControllerURL accepts only the URL shape produced by the local
// mock Controller used by integration/lifecycle tests. The strict shape keeps
// E2E configuration fail-closed and avoids DNS or conventional real-controller
// endpoint ambiguity.
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
