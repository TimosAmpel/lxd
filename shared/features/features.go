// Package features provides process-local gates for work-in-progress features.
// Feature gates expose unsupported and unfinished functionality. They MUST NOT
// be enabled in production environments.
//
// Applications load enabled gates from the environment during startup:
//
//	err := features.LoadFromEnv(features.EnvVar)
//
// A feature declares a typed name and checks it before exposing gated code:
//
//	const experimentalFeature features.Feature = "experimental_feature"
//	if features.IsOpen(experimentalFeature) {
//		registerExperimentalFeature()
//	}
//
// Gates are closed by default. Add a feature constant only with a real consumer,
// and remove its gate when the feature is ready for general use.
package features

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Feature identifies an unsupported, work-in-progress feature.
type Feature string

// EnvVar is the environment variable which holds the active features of LXD.
const EnvVar = "LXD_FEATURES"

var (
	mu   sync.RWMutex
	open = map[Feature]struct{}{}
)

// LoadFromEnv replaces the set of open feature gates from the environment.
func LoadFromEnv(envVar string) error {
	var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

	value := os.Getenv(envVar)

	next := make(map[Feature]struct{})
	if value != "" {
		for _, name := range strings.Split(value, ",") {
			if !namePattern.MatchString(name) {
				return fmt.Errorf("Invalid feature gate %q", name)
			}

			next[Feature(name)] = struct{}{}
		}
	}

	mu.Lock()
	open = next
	mu.Unlock()

	return nil
}

// IsOpen reports whether feature is enabled.
func IsOpen(feature Feature) bool {
	mu.RLock()
	_, found := open[feature]
	mu.RUnlock()

	return found
}
