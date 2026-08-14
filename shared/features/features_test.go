package features

import (
	"testing"
)

func TestLoadFromEnv(t *testing.T) {
	const envVar = "LXD_TEST_FEATURES"

	tests := []struct {
		name       string
		value      string
		wantOpen   []Feature
		wantClosed []Feature
	}{
		{
			name:       "empty",
			wantClosed: []Feature{"test_feature_a", "test_feature_b"},
		},
		{
			name:       "one feature",
			value:      "test_feature_a",
			wantOpen:   []Feature{"test_feature_a"},
			wantClosed: []Feature{"test_feature_b"},
		},
		{
			name:     "multiple features",
			value:    "test_feature_a,test_feature_b",
			wantOpen: []Feature{"test_feature_a", "test_feature_b"},
		},
		{
			name:     "duplicate feature",
			value:    "test_feature_a,test_feature_a",
			wantOpen: []Feature{"test_feature_a"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(envVar, test.value)

			err := LoadFromEnv(envVar)
			if err != nil {
				t.Fatalf("LoadFromEnv() returned an unexpected error: %v", err)
			}

			for _, feature := range test.wantOpen {
				if !IsOpen(feature) {
					t.Errorf("Expected feature %q to be open", feature)
				}
			}

			for _, feature := range test.wantClosed {
				if IsOpen(feature) {
					t.Errorf("Expected feature %q to be closed", feature)
				}
			}
		})
	}
}

func TestLoadFromEnvRejectsInvalidNames(t *testing.T) {
	const envVar = "LXD_TEST_FEATURES"

	tests := []string{
		"test-feature",
		"test feature",
		",test_feature",
		"test_feature,",
		"test_feature_a,,test_feature_b",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv(envVar, value)

			err := LoadFromEnv(envVar)
			if err == nil {
				t.Fatalf("Expected LoadFromEnv() to reject %q", value)
			}
		})
	}
}

func TestLoadFromEnvReplacesState(t *testing.T) {
	const envVar = "LXD_TEST_FEATURES"

	t.Setenv(envVar, "test_feature_a")
	err := LoadFromEnv(envVar)
	if err != nil {
		t.Fatalf("Failed loading initial state: %v", err)
	}

	t.Setenv(envVar, "test_feature_b")
	err = LoadFromEnv(envVar)
	if err != nil {
		t.Fatalf("Failed loading replacement state: %v", err)
	}

	if IsOpen("test_feature_a") {
		t.Error("Expected replaced feature to be closed")
	}

	if !IsOpen("test_feature_b") {
		t.Error("Expected replacement feature to be open")
	}
}

func TestLoadFromEnvFailurePreservesState(t *testing.T) {
	const envVar = "LXD_TEST_FEATURES"

	t.Setenv(envVar, "test_feature_a")
	err := LoadFromEnv(envVar)
	if err != nil {
		t.Fatalf("Failed loading initial state: %v", err)
	}

	t.Setenv(envVar, "test_feature_b,invalid-name")
	err = LoadFromEnv(envVar)
	if err == nil {
		t.Fatal("Expected LoadFromEnv() to reject invalid state")
	}

	if !IsOpen("test_feature_a") {
		t.Error("Expected initial feature to remain open")
	}

	if IsOpen("test_feature_b") {
		t.Error("Expected partially parsed feature to remain closed")
	}
}
