// Package features provides process-local feature previews.
// Feature previews expose unsupported and unfinished functionality. They MUST NOT
// be enabled in production environments.
//
// Applications load enabled previews from the environment during startup.
// The IsEnabled function is used though source code to checks if the feature
// is enabled and activates related code:
//
//	if features.IsEnabled(features.FeatureX) {
//		...
//	}
//
// Where FeatureX is a defined constant in features' package.
//
// Feature previews are disabled by default. Add a new feature constant when its
// implementation is still work-in-progress and remove the conditional code enablement
// only when the feature is ready for general use.
package features

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"maps"
	"slices"
)

// Feature identifies an unsupported, work-in-progress feature.
type Feature string

const (
	// envVar is the environment variable which holds the active features of LXD.
	envVar = "LXD_FEATURES"
	// New supported feature previews should be defined as constances.
	// Ex. FeatureX Feature = "feature_x"
	 FeatureTest Feature = "feature_test"
)

var (
	// enabledFeaturePrevs holds the status of supported feature previews.
	// Example:
	// enabledFeaturePrevs = map[Feature]bool{
	// 	FeatureX: false
	// }
	enabledFeaturePrevs = map[Feature]bool{
		FeatureTest: false,
	}
)

func printAvailFeatures() {
	fmt.Print("Available features are: ")
	prevs := slices.Collect(maps.Keys(enabledFeaturePrevs))
	last := len(prevs) - 1
	for _, prev := range prevs[:last] {
		fmt.Print(prev, ",")
	}
	fmt.Println(prevs[last])
}

func init() {
	err := loadFromEnv(envVar)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		printAvailFeatures()
		os.Exit(1)
	}
}

// LoadFromEnv replaces the set of enabled feature previews from the environment.
func loadFromEnv(envVar string) error {
	var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

	value := os.Getenv(envVar)
	if value != "" {
		for _, name := range strings.Split(value, ",") {
			// Check if feature name pattern in acceptable form
			if !namePattern.MatchString(name) {
				return fmt.Errorf("Invalid feature preview %q", name)
			}

			// Check if feature name exists in supported list features
			enabled, exist := enabledFeaturePrevs[Feature(name)]
			if !exist {
				return fmt.Errorf("Feature preview named \"%q\" is not supported", name)
			}

			// Check if feature name has already been provided
			if enabled {
				return fmt.Errorf("Feature preview %q has already been enabled", name)
			}

			// Enabled the feature preview
			enabledFeaturePrevs[Feature(name)] = true
		}
	}

	return nil
}

// IsEnabled reports whether the feature preview is enabled.
func IsEnabled(feature Feature) bool {
	enabled, found := enabledFeaturePrevs[feature]
	if !found {
		fmt.Fprintf(os.Stderr, "Feature preview %s not found!", feature)
		printAvailFeatures()
		return false
	}

	return enabled
}
