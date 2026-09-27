package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/canonical/lxd/lxd/instance/drivers/libkrun"
)

func TestParseHWAddr(t *testing.T) {
	tests := []struct {
		hwaddr string
		mac    [6]byte
		err    bool
	}{
		{
			hwaddr: "00:16:3e:11:22:33",
			mac:    [6]byte{0x00, 0x16, 0x3e, 0x11, 0x22, 0x33},
			err:    false,
		},
		{
			hwaddr: "aa:bb:cc:dd:ee:ff",
			mac:    [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			err:    false,
		},
		{
			hwaddr: "invalid-format",
			err:    true,
		},
		{
			hwaddr: "",
			err:    true,
		},
		{
			hwaddr: "00:16:3e:11:22:33:44:55",
			err:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.hwaddr, func(t *testing.T) {
			mac, err := parseHWAddr(tt.hwaddr)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mac, mac)
			}
		})
	}
}

func TestDetectKernelFormat(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name     string
		magic    []byte
		expected libkrun.KernelFormat
		err      bool
	}{
		{
			name:     "ELF vmlinux",
			magic:    []byte{0x7f, 'E', 'L', 'F'},
			expected: libkrun.KernelFormatELF,
			err:      false,
		},
		{
			name:     "x86 PE bzImage",
			magic:    []byte{'M', 'Z', 0x00, 0x00},
			expected: libkrun.KernelFormatPEGZ,
			err:      false,
		},
		{
			name:     "gzip image",
			magic:    []byte{0x1f, 0x8b, 0x08, 0x00},
			expected: libkrun.KernelFormatImageGZ,
			err:      false,
		},
		{
			name:     "zstd image",
			magic:    []byte{0x28, 0xb5, 0x2f, 0xfd},
			expected: libkrun.KernelFormatImageZstd,
			err:      false,
		},
		{
			name:     "bzip2 image",
			magic:    []byte{'B', 'Z', 'h', '9'},
			expected: libkrun.KernelFormatImageBZ2,
			err:      false,
		},
		{
			name:     "unknown image format",
			magic:    []byte{0x00, 0x00, 0x00, 0x00},
			expected: 0,
			err:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tempDir, "kernel-"+tt.name)
			require.NoError(t, os.WriteFile(filePath, tt.magic, 0600))

			format, err := detectKernelFormat(filePath)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, format)
			}
		})
	}
}

func TestWriteExitFile(t *testing.T) {
	tempDir := t.TempDir()
	exitPath := filepath.Join(tempDir, "libkrun.exit")

	require.NoError(t, writeExitFile(exitPath, 1234, "reboot"))
	content, err := os.ReadFile(exitPath)
	require.NoError(t, err)
	assert.Equal(t, "pid=1234 target=reboot\n", string(content))

	// Overwrite atomically with stop.
	require.NoError(t, writeExitFile(exitPath, 1234, "stop"))
	content, err = os.ReadFile(exitPath)
	require.NoError(t, err)
	assert.Equal(t, "pid=1234 target=stop\n", string(content))
}

func TestLibkrunChildExitTarget(t *testing.T) {
	tests := []struct {
		name       string
		waitStatus syscall.WaitStatus
		target     string
		exitCode   int
	}{
		{name: "guest reboot", waitStatus: syscall.WaitStatus(0), target: "reboot", exitCode: 0},
		{name: "guest halted", waitStatus: syscall.WaitStatus(1 << 8), target: "stop", exitCode: 1},
		{name: "libkrun error", waitStatus: syscall.WaitStatus(2 << 8), target: "stop", exitCode: 2},
		{name: "killed", waitStatus: syscall.WaitStatus(syscall.SIGKILL), target: "stop", exitCode: 128 + int(syscall.SIGKILL)},
		{name: "terminated", waitStatus: syscall.WaitStatus(syscall.SIGTERM), target: "stop", exitCode: 128 + int(syscall.SIGTERM)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, exitCode := libkrunChildExitTarget(tt.waitStatus)
			assert.Equal(t, tt.target, target)
			assert.Equal(t, tt.exitCode, exitCode)
		})
	}
}
