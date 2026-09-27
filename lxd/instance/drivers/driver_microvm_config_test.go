package drivers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMicroVMConfigRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, MicroVMConfigFileName)

	cfg := MicroVMConfig{
		CPUs:      2,
		MemoryMiB: 1024,
		Kernel: MicroVMConfigKernel{
			Path:    "/path/to/vmlinux",
			Format:  "auto",
			Cmdline: "console=hvc0 root=/dev/vda rw",
		},
		RootDisk:    "/path/to/root.img",
		ConfigDrive: "/path/to/config.mount",
		Console:     "/path/to/libkrun.console",
		ExitFile:    "/path/to/libkrun.exit",
		NICs: []MicroVMConfigNIC{
			{
				Tap:    "tap0",
				HWAddr: "00:16:3e:11:22:33",
			},
		},
		Vsock: &MicroVMConfigVsock{
			AgentSocket: "/path/to/agent.sock",
			LXDPort:     8443,
			LXDSocket:   "/path/to/lxd.sock",
		},
	}

	err := WriteMicroVMConfig(configPath, cfg)
	require.NoError(t, err)

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0640), info.Mode().Perm())

	loaded, err := ReadMicroVMConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, &cfg, loaded)
}

func TestMicroVMConfigUnknownFieldsRejected(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, MicroVMConfigFileName)

	invalidJSON := `{
  "cpus": 1,
  "memory_mib": 512,
  "kernel": {
    "path": "/vmlinux",
    "format": "auto",
    "cmdline": "console=hvc0"
  },
  "root_disk": "/root.img",
  "config_drive": "/config",
  "console": "/console",
  "exit_file": "/exit",
  "unknown_property": "should_fail"
}`

	err := os.WriteFile(configPath, []byte(invalidJSON), 0640)
	require.NoError(t, err)

	_, err = ReadMicroVMConfig(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
}

func TestMicroVMConfigValidate(t *testing.T) {
	baseValid := func() MicroVMConfig {
		return MicroVMConfig{
			CPUs:      1,
			MemoryMiB: 512,
			Kernel: MicroVMConfigKernel{
				Path:    "/vmlinux",
				Format:  "auto",
				Cmdline: "console=hvc0",
			},
			RootDisk:    "/root.img",
			ConfigDrive: "/config",
			Console:     "/console",
			ExitFile:    "/exit",
		}
	}

	tests := []struct {
		name    string
		mutate  func(c *MicroVMConfig)
		wantErr string
	}{
		{
			name:    "valid base",
			mutate:  func(_ *MicroVMConfig) {},
			wantErr: "",
		},
		{
			name: "missing cpus",
			mutate: func(c *MicroVMConfig) {
				c.CPUs = 0
			},
			wantErr: "cpus",
		},
		{
			name: "missing memory",
			mutate: func(c *MicroVMConfig) {
				c.MemoryMiB = 0
			},
			wantErr: "memory_mib",
		},
		{
			name: "missing kernel path",
			mutate: func(c *MicroVMConfig) {
				c.Kernel.Path = ""
			},
			wantErr: "kernel.path",
		},
		{
			name: "missing root disk",
			mutate: func(c *MicroVMConfig) {
				c.RootDisk = ""
			},
			wantErr: "root_disk",
		},
		{
			name: "missing config drive",
			mutate: func(c *MicroVMConfig) {
				c.ConfigDrive = ""
			},
			wantErr: "config_drive",
		},
		{
			name: "missing console",
			mutate: func(c *MicroVMConfig) {
				c.Console = ""
			},
			wantErr: "console",
		},
		{
			name: "invalid nic missing tap",
			mutate: func(c *MicroVMConfig) {
				c.NICs = []MicroVMConfigNIC{{HWAddr: "00:16:3e:11:22:33"}}
			},
			wantErr: "missing tap or hwaddr",
		},
		{
			name: "incomplete vsock missing lxd socket",
			mutate: func(c *MicroVMConfig) {
				c.Vsock = &MicroVMConfigVsock{
					AgentSocket: "/agent.sock",
					LXDPort:     1234,
				}
			},
			wantErr: "vsock",
		},
		{
			name: "valid with vsock and nic",
			mutate: func(c *MicroVMConfig) {
				c.NICs = []MicroVMConfigNIC{{Tap: "tap0", HWAddr: "00:16:3e:11:22:33"}}
				c.Vsock = &MicroVMConfigVsock{
					AgentSocket: "/agent.sock",
					LXDPort:     1234,
					LXDSocket:   "/lxd.sock",
				}
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValid()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
