package instancetype

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigKeyCheckerMicroVM(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		// Valid keys for MicroVM (inherited from Any or VM)
		{"limits.cpu", true},
		{"limits.memory", true},
		{"security.protection.delete", true},
		{"raw.apparmor", true},
		{"user.my-key", true},
		{"environment.MY_VAR", true},

		// Container-only keys must not be valid for MicroVM
		{"raw.lxc", false},
		{"raw.seccomp", false},
		{"security.nesting", false},
		{"security.privileged", false},
		{"security.protection.shift", false},

		// Unknown / random keys
		{"invalid.key", false},
		{"random_config_key", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, err := ConfigKeyChecker(tt.key, MicroVM)
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
