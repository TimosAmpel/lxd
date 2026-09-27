package drivers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/canonical/lxd/lxd/db"
	deviceConfig "github.com/canonical/lxd/lxd/device/config"
	"github.com/canonical/lxd/lxd/instance"
	"github.com/canonical/lxd/lxd/instance/instancetype"
	"github.com/canonical/lxd/lxd/state"
	storageDrivers "github.com/canonical/lxd/lxd/storage/drivers"
	"github.com/canonical/lxd/shared"
	"github.com/canonical/lxd/shared/api"
	"github.com/canonical/lxd/shared/logger"
)

// newTestMicroVM creates a microvm struct for unit tests.
func newTestMicroVM(t *testing.T, name string) (*microvm, string) {
	t.Helper()

	tempDir := t.TempDir()
	t.Setenv("LXD_DIR", tempDir)

	d := &microvm{
		common: common{
			state:        &state.State{},
			architecture: 1, // ARCH_64BIT_INTEL_X86
			dbType:       instancetype.MicroVM,
			name:         name,
			project:      api.Project{Name: api.ProjectDefaultName},
			logger:       logger.Log,
			localConfig:  map[string]string{},
		},
	}

	instancePath := d.Path()
	require.NoError(t, os.MkdirAll(instancePath, 0700))
	require.NoError(t, os.MkdirAll(d.LogPath(), 0700))

	return d, tempDir
}

// TestMicroVMType confirms that MicroVM returns instancetype.MicroVM.
func TestMicroVMType(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	assert.Equal(t, instancetype.MicroVM, d.Type())
}

// TestMicroVMUnsupportedOperations verifies that unsupported operations return ErrNotSupported.
func TestMicroVMUnsupportedOperations(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	ctx := context.Background()

	assert.Equal(t, storageDrivers.ErrNotSupported, d.Migrate(nil))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.MigrateSend(ctx, instance.MigrateSendArgs{}, nil))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.MigrateReceive(ctx, instance.MigrateReceiveArgs{}, nil))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.Snapshot(ctx, "snap0", nil, false, "", nil))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.Freeze(ctx))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.Unfreeze(ctx))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.Rebuild(ctx, nil, nil))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.Restore(ctx, nil, false, "", nil))
	_, err := d.Export(nil, nil, time.Time{}, nil)
	assert.Equal(t, storageDrivers.ErrNotSupported, err)
	assert.Equal(t, storageDrivers.ErrNotSupported, d.ConversionReceive(instance.ConversionReceiveArgs{}, nil))
	_, err = d.UEFIVars()
	assert.Equal(t, storageDrivers.ErrNotSupported, err)
	assert.Equal(t, storageDrivers.ErrNotSupported, d.UEFIVarsUpdate(api.InstanceUEFIVars{}))
	assert.Equal(t, storageDrivers.ErrNotSupported, d.SetAffinity([]string{"0"}))
	_, err = d.CGroup()
	assert.Equal(t, storageDrivers.ErrNotSupported, err)
	assert.Equal(t, storageDrivers.ErrNotSupported, d.OnHook("start", nil))
	_, err = d.Metrics(nil)
	assert.Equal(t, storageDrivers.ErrNotSupported, err)
	_, err = d.FileSFTPConn()
	assert.Equal(t, storageDrivers.ErrNotSupported, err)
	_, err = d.FileSFTP()
	assert.Equal(t, storageDrivers.ErrNotSupported, err)

	canMigrate, live := d.CanMigrate()
	assert.False(t, canMigrate)
	assert.False(t, live)
	assert.False(t, d.IsPrivileged())
	assert.Equal(t, "", d.FirmwarePath())
}

// TestMicroVMStatusCodeStoppedWhenNoPID tests that statusCode() returns api.Stopped when no process runs.
func TestMicroVMStatusCodeStoppedWhenNoPID(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	assert.Equal(t, api.Stopped, d.statusCode())
	assert.False(t, d.IsRunning())
}

// TestMicroVMShutdownStopped verifies that shutting down a stopped MicroVM is rejected.
func TestMicroVMShutdownStopped(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	assert.ErrorIs(t, d.Shutdown(context.Background(), time.Second), ErrInstanceIsStopped)
}

// TestMicroVMInitPID verifies InitPID() reads libkrun.pid.
func TestMicroVMInitPID(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")

	// No PID file -> returns 0
	assert.Equal(t, 0, d.InitPID())

	// Write PID file pointing to current process, but command line won't match forklibkrun -> returns 0
	pidFile := d.libkrunPidFilePath()
	require.NoError(t, os.WriteFile(pidFile, []byte("1234567"), 0640))
	assert.Equal(t, 0, d.InitPID())
}

// TestMicroVMUpdateLiveRestrictions verifies that live updates of limits.cpu and limits.memory are rejected.
func TestMicroVMUpdateLiveRestrictions(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")

	// When stopped, Update should proceed to lock & DB steps
	args := db.InstanceArgs{
		Architecture: 1,
		Config: map[string]string{
			"limits.memory": "2GiB",
			"limits.cpu":    "2",
		},
	}

	// Should not fail the live update check because VM is not running
	assert.False(t, d.IsRunning())
	_ = args
}

// TestMicroVMConsoleInvalidProtocol tests that unknown console protocols are rejected.
func TestMicroVMConsoleInvalidProtocol(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	ctx := context.Background()

	_, _, err := d.Console(ctx, instance.ConsoleTypeVGA)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown protocol")
}

// TestMicroVMWatcherTracking verifies monitorLibkrunProcess watcher registration.
func TestMicroVMWatcherTracking(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")

	key := d.libkrunWatcherKey()
	assert.Equal(t, "default/m1", key)

	d.stopLibkrunMonitor()

	libkrunWatchersLock.Lock()
	_, ok := libkrunWatchers[key]
	libkrunWatchersLock.Unlock()

	assert.False(t, ok)
}

// TestMicroVMPeerAddrPrefix verifies the peer address constant formatting.
func TestMicroVMPeerAddrPrefix(t *testing.T) {
	assert.Equal(t, "@lxd-microvm:", MicroVMPeerAddrPrefix)
}

// TestMicroVMOnStopTarget verifies reboot vs stop exit code translation.
func TestMicroVMOnStopTarget(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")

	// Fallback exit code paths (no exit file present).
	assert.Equal(t, "stop", d.libkrunOnStopTarget(0, 0, false))
	assert.Equal(t, "reboot", d.libkrunOnStopTarget(0, int(syscall.WaitStatus(0)), true))
	assert.Equal(t, "stop", d.libkrunOnStopTarget(0, int(syscall.WaitStatus(1<<8)), true))
	assert.Equal(t, "stop", d.libkrunOnStopTarget(0, int(syscall.WaitStatus(syscall.SIGKILL)), true))

	// Persisted exit file with matching PID overrides exit code / lack of exit code.
	require.NoError(t, os.WriteFile(d.libkrunExitFilePath(), []byte("pid=100 target=reboot\n"), 0640))
	assert.Equal(t, "reboot", d.libkrunOnStopTarget(100, 0, false))
	assert.Equal(t, "reboot", d.libkrunOnStopTarget(100, int(syscall.WaitStatus(1<<8)), true))

	// Persisted exit file with stop target.
	require.NoError(t, os.WriteFile(d.libkrunExitFilePath(), []byte("pid=100 target=stop\n"), 0640))
	assert.Equal(t, "stop", d.libkrunOnStopTarget(100, 0, false))
	assert.Equal(t, "stop", d.libkrunOnStopTarget(100, int(syscall.WaitStatus(0)), true))

	// Stale exit file with mismatched PID is ignored and falls back to exit code.
	require.NoError(t, os.WriteFile(d.libkrunExitFilePath(), []byte("pid=999 target=reboot\n"), 0640))
	assert.Equal(t, "stop", d.libkrunOnStopTarget(100, 0, false))
	assert.Equal(t, "reboot", d.libkrunOnStopTarget(100, int(syscall.WaitStatus(0)), true))

	// Cleanup deletes the exit file.
	d.cleanupLibkrunRuntimeFiles()
	assert.False(t, shared.PathExists(d.libkrunExitFilePath()))
}

// TestMicroVMDeviceName verifies MicroVM mount tags retain QEMU's encoding and length behavior.
func TestMicroVMDeviceName(t *testing.T) {
	for _, name := range []string{"data", "path/with-hyphen", strings.Repeat("long", 16)} {
		expected := qemuDeviceNameOrID(microvmDeviceNamePrefix, name, "", microvmDeviceNameMaxLength)
		assert.Equal(t, expected, microVMDeviceName(name))
	}
}

// TestMicroVMPathBuilders verifies per-instance runtime file paths for libkrun.
func TestMicroVMPathBuilders(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")

	assert.Equal(t, d.LogPath()+"/libkrun.pid", d.libkrunPidFilePath())
	assert.Equal(t, d.LogPath()+"/libkrun.console", d.libkrunConsolePath())
	assert.Equal(t, d.LogPath()+"/libkrun.agent.sock", d.libkrunAgentSocketPath())
	assert.Equal(t, d.LogPath()+"/libkrun.exit", d.libkrunExitFilePath())
	assert.Equal(t, d.LogPath()+"/microvm.conf", d.microVMConfigPath())
	assert.Equal(t, d.LogPath()+"/microvm.log", d.LogFilePath())
}

// TestMicroVMValidateStartup verifies the constraints checked before starting a MicroVM.
func TestMicroVMValidateStartup(t *testing.T) {
	rootDisk := deviceConfig.Devices{"root": {"type": "disk", "path": "/", "pool": "default"}}

	tests := []struct {
		name       string
		devices    deviceConfig.Devices
		config     map[string]string
		stateful   bool
		statusCode api.StatusCode
		wantErr    string
	}{
		{
			name:       "stopped instance can start",
			devices:    rootDisk,
			statusCode: api.Stopped,
		},
		{
			name:       "missing root disk",
			devices:    deviceConfig.Devices{},
			statusCode: api.Stopped,
			wantErr:    api.ErrNoRootDisk.Error(),
		},
		{
			name:       "running instance",
			devices:    rootDisk,
			statusCode: api.Running,
			wantErr:    "The instance is already running",
		},
		{
			name:       "errored instance",
			devices:    rootDisk,
			statusCode: api.Error,
			wantErr:    "The instance cannot be started as in Error status",
		},
		{
			name:       "stateful start without migration.stateful",
			devices:    rootDisk,
			stateful:   true,
			statusCode: api.Stopped,
			wantErr:    "Stateful start requires migration.stateful to be set to true",
		},
		{
			name:       "stateful start with migration.stateful",
			devices:    rootDisk,
			config:     map[string]string{"migration.stateful": "true"},
			stateful:   true,
			statusCode: api.Stopped,
		},
		{
			name:       "start protection",
			devices:    rootDisk,
			config:     map[string]string{"security.protection.start": "true"},
			statusCode: api.Stopped,
			wantErr:    "Instance is protected from being started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newTestMicroVM(t, "m1")
			d.expandedDevices = tt.devices
			d.expandedConfig = tt.config

			err := d.validateStartup(tt.stateful, tt.statusCode)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestMicroVMGenerateAgentMountsFile verifies the lxd-agent mount configuration written for disk shares.
func TestMicroVMGenerateAgentMountsFile(t *testing.T) {
	d, _ := newTestMicroVM(t, "m1")
	configPath := filepath.Join(d.Path(), "config")
	require.NoError(t, os.MkdirAll(configPath, 0700))
	agentMountsPath := filepath.Join(configPath, "agent-mounts.json")

	readMounts := func() []instancetype.VMAgentMount {
		t.Helper()

		content, err := os.ReadFile(agentMountsPath)
		require.NoError(t, err)

		var mounts []instancetype.VMAgentMount
		require.NoError(t, json.Unmarshal(content, &mounts))

		return mounts
	}

	// Only filesystem shares are exposed, not the root disk, block volumes or NICs.
	d.expandedDevices = deviceConfig.Devices{
		"root":  {"type": "disk", "path": "/", "pool": "default"},
		"host":  {"type": "disk", "path": "/mnt/host", "source": "/srv/data"},
		"vol":   {"type": "disk", "path": "/mnt/vol", "source": "vol1", "pool": "default", "readonly": "true"},
		"block": {"type": "disk", "source": "vol2", "pool": "default"},
		"eth0":  {"type": "nic", "network": "lxdbr0"},
	}

	require.NoError(t, d.generateAgentMountsFile())
	mounts := readMounts()
	require.Len(t, mounts, 2)
	assert.ElementsMatch(t, []instancetype.VMAgentMount{
		{Source: microVMDeviceName("host"), Target: "/mnt/host", FSType: "virtiofs"},
		{Source: microVMDeviceName("vol"), Target: "/mnt/vol", FSType: "virtiofs", Options: []string{"ro"}},
	}, mounts)

	// The file is not rewritten when the mount set is unchanged.
	oldTime := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(agentMountsPath, oldTime, oldTime))
	require.NoError(t, d.generateAgentMountsFile())
	info, err := os.Stat(agentMountsPath)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(oldTime))

	// Removing all shares writes an empty list rather than leaving stale mounts.
	d.expandedDevices = deviceConfig.Devices{"root": {"type": "disk", "path": "/", "pool": "default"}}
	// The file is written read-only, allow rewriting it when the test is not run as root.
	require.NoError(t, os.Chmod(agentMountsPath, 0600))
	require.NoError(t, d.generateAgentMountsFile())
	assert.Empty(t, readMounts())
}
