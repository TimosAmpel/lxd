# MicroVM test helper to check pre-conditions.
# Sets TEST_UNMET_REQUIREMENT and returns 1 if the host cannot run MicroVMs.
_microvm_check_prerequisites() {
  if [ ! -e "/dev/kvm" ]; then
    export TEST_UNMET_REQUIREMENT="MicroVM tests require KVM support (/dev/kvm missing)"
    return 1
  fi

  if [ ! -f "${LXD_DIR}/microvm/vmlinuz" ]; then
    export TEST_UNMET_REQUIREMENT="MicroVM kernel not found, set LXD_MICROVM_KERNEL"
    return 1
  fi

  # Mirror the lookup order used by lxd/instance/drivers/libkrun/dynload.c.
  local has_libkrun=false
  if [ -n "${LIBKRUN_PATH:-}" ] && [ -f "${LIBKRUN_PATH}" ]; then
    has_libkrun=true
  elif ldconfig -p 2>/dev/null | grep -E '\slibkrun\.so(\.0)? ' >/dev/null; then
    has_libkrun=true
  fi

  if [ "${has_libkrun}" = "false" ]; then
    export TEST_UNMET_REQUIREMENT="libkrun not found, install it or set LIBKRUN_PATH"
    return 1
  fi

  if [ "$(storage_backend "${LXD_DIR}")" != "dir" ]; then
    export TEST_UNMET_REQUIREMENT="MicroVM tests currently require the dir storage backend"
    return 1
  fi

  return 0
}

# Runs a command that is expected to fail and checks its output contains the expected text.
_microvm_assert_fails_with() {
  local expected="${1}"
  shift

  local output
  output="$(! "$@" 2>&1 || false)"
  if ! grep -F -- "${expected}" <<< "${output}" >/dev/null; then
    echo "ERROR: expected output to contain ${expected@Q}, got ${output@Q}"
    return 1
  fi
}

test_microvm_feature_gate() {
  local LXD_DIR
  LXD_DIR="$(mktemp -d -p "${TEST_DIR}" XXX)"

  sub_test "Verify the client hides --microvm when the feature preview is disabled"
  ! LXD_FEATURES="" lxc init --help | grep -wF -- "--microvm" || false
  LXD_FEATURES="microvm" lxc init --help | grep -wF -- "--microvm" >/dev/null
  LXD_FEATURES="" _microvm_assert_fails_with "unknown flag: --microvm" lxc init testimage m-gated --microvm

  sub_test "Verify the daemon rejects MicroVMs when the feature preview is disabled"
  LXD_FEATURES="" spawn_lxd "${LXD_DIR}" true
  ensure_import_testimage
  LXD_FEATURES="microvm" _microvm_assert_fails_with 'Instance type "microvm" requires the "microvm" feature preview' lxc init testimage m-gated --microvm
  ! lxc info m-gated || false

  kill_lxd "${LXD_DIR}"
}

test_microvm_lifecycle() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  sub_test "Invalid MicroVM names"
  ! lxc init testimage ".." --microvm || false
  ! lxc init testimage "/" --microvm || false

  sub_test "Test MicroVM init and status reporting"
  lxc init testimage m1 --microvm -c limits.memory=512MiB
  [ "$(lxc list -f csv -c t m1)" = "MICROVM" ]
  [ "$(lxc list -f csv -c s m1)" = "STOPPED" ]
  lxc query /1.0/instances/m1 | jq --exit-status '.type == "microvm"'
  [ -d "${LXD_DIR}/containers/m1" ]

  sub_test "Test config update while stopped"
  lxc config set m1 limits.memory=1GiB limits.cpu=2
  [ "$(lxc config get m1 limits.memory)" = "1GiB" ]
  [ "$(lxc config get m1 limits.cpu)" = "2" ]

  sub_test "Test rename while stopped"
  lxc rename m1 m2
  [ "$(lxc list -f csv -c t m2)" = "MICROVM" ]
  [ "$(lxc list -f csv -c s m2)" = "STOPPED" ]
  ! lxc info m1 || false

  sub_test "Test MicroVM storage volume reporting"
  local pool_name
  pool_name="$(lxc profile device get default root pool)"
  lxc storage volume list "${pool_name}" -f csv -c t,n | grep -xF "microvm,m2"
  lxc storage volume show "${pool_name}" microvm/m2 | grep -xF "type: microvm"

  sub_test "Test start protection"
  lxc config set m2 security.protection.start=true
  _microvm_assert_fails_with "Instance is protected from being started" lxc start m2
  [ "$(lxc list -f csv -c s m2)" = "STOPPED" ]
  lxc config unset m2 security.protection.start

  sub_test "Test start and running state"
  lxc start m2
  [ "$(lxc list -f csv -c s m2)" = "RUNNING" ]
  lxc query /1.0/instances/m2/state | jq --exit-status '.pid > 0'

  sub_test "Test libkrun helper process and log file"
  local pid_before
  pid_before="$(< "${LXD_DIR}/logs/m2/libkrun.pid")"
  [ -n "${pid_before}" ]
  kill -0 "${pid_before}"
  grep -aF forklibkrun "/proc/${pid_before}/cmdline" >/dev/null
  [ -f "${LXD_DIR}/logs/m2/microvm.log" ]

  sub_test "Test microvm.conf is generated and exposed through the logs API"
  [ -f "${LXD_DIR}/logs/m2/microvm.conf" ]
  curl --unix-socket "${LXD_DIR}/unix.socket" "http://localhost/1.0/instances/m2/logs/microvm.conf" | jq --exit-status '.cpus == 2 and .memory_mib == 1024 and .kernel.format == "auto"'
  lxc query /1.0/instances/m2/logs | jq --exit-status 'any(.[]; endswith("/microvm.conf"))'
  lxc query /1.0/instances/m2/logs | jq --exit-status 'any(.[]; endswith("/microvm.log"))'
  grep -aF -- "--config" "/proc/${pid_before}/cmdline" >/dev/null
  ! grep -aF -- "--kernel" "/proc/${pid_before}/cmdline" || false
  lxc info m2 --show-log >/dev/null

  sub_test "Test operations rejected on a running MicroVM"
  _microvm_assert_fails_with "The instance is already running" lxc start m2
  _microvm_assert_fails_with 'Key "limits.memory" cannot be updated when VM is running' lxc config set m2 limits.memory=2GiB
  _microvm_assert_fails_with 'Key "limits.cpu" cannot be updated when VM is running' lxc config set m2 limits.cpu=1
  [ "$(lxc config get m2 limits.memory)" = "1GiB" ]
  ! lxc rename m2 m3 || false
  ! lxc delete m2 || false
  [ "$(lxc list -f csv -c s m2)" = "RUNNING" ]

  sub_test "Test graceful stop and restart require lxd-agent"
  # testimage has no lxd-agent, so the guest cannot be asked to power off.
  _microvm_assert_fails_with "Failed requesting guest power off" lxc stop m2 --timeout 10
  _microvm_assert_fails_with "Failed requesting guest power off" lxc restart m2 --timeout 10
  [ "$(lxc list -f csv -c s m2)" = "RUNNING" ]
  [ "$(< "${LXD_DIR}/logs/m2/libkrun.pid")" = "${pid_before}" ]

  sub_test "Test restart spawns a new libkrun helper process"
  lxc restart -f m2
  [ "$(lxc list -f csv -c s m2)" = "RUNNING" ]
  local pid_after
  pid_after="$(< "${LXD_DIR}/logs/m2/libkrun.pid")"
  [ -n "${pid_after}" ]
  [ "${pid_before}" != "${pid_after}" ]
  ! kill -0 "${pid_before}" 2>/dev/null || false

  sub_test "Test force stop cleans up the libkrun helper process"
  lxc stop -f m2
  [ "$(lxc list -f csv -c s m2)" = "STOPPED" ]
  ! kill -0 "${pid_after}" 2>/dev/null || false
  _microvm_assert_fails_with "The instance is already stopped" lxc stop -f m2
  [ -f "${LXD_DIR}/logs/m2/microvm.conf" ]

  sub_test "Test live update is allowed again after stop"
  lxc config set m2 limits.memory=512MiB
  [ "$(lxc config get m2 limits.memory)" = "512MiB" ]

  sub_test "Test start and delete while running with --force"
  lxc start m2
  [ "$(lxc list -f csv -c s m2)" = "RUNNING" ]
  lxc delete -f m2
  ! lxc info m2 || false
  [ ! -d "${LXD_DIR}/logs/m2" ]
  [ ! -e "${LXD_DIR}/containers/m2" ]
}

test_microvm_missing_kernel() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  local kernel="${LXD_DIR}/microvm/vmlinuz"
  lxc init testimage m-nokernel --microvm

  sub_test "Test start fails cleanly when the MicroVM kernel is missing"
  mv "${kernel}" "${kernel}.bak"
  _microvm_assert_fails_with "Kernel not found at" lxc start m-nokernel
  [ "$(lxc list -f csv -c s m-nokernel)" = "STOPPED" ]
  mv "${kernel}.bak" "${kernel}"

  sub_test "Test start succeeds once the kernel is restored"
  lxc start m-nokernel
  [ "$(lxc list -f csv -c s m-nokernel)" = "RUNNING" ]
  lxc delete -f m-nokernel
}

test_microvm_devices_disk() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  local share="${TEST_DIR}/m-disk-share"
  local agent_mounts="${LXD_DIR}/containers/m-disk/config/agent-mounts.json"
  mkdir -p "${share}"
  echo "test data" > "${share}/hello.txt"

  sub_test "Test MicroVM starts with a host directory disk added while stopped"
  lxc init testimage m-disk --microvm
  lxc config device add m-disk data1 disk source="${share}" path=/mnt/data1
  lxc start m-disk
  [ "$(lxc list -f csv -c s m-disk)" = "RUNNING" ]

  sub_test "Test hot-adding a disk regenerates agent-mounts.json"
  lxc config device add m-disk data2 disk source="${share}" path=/mnt/data2 readonly=true
  [ -f "${agent_mounts}" ]
  jq --exit-status 'map(.target) | sort == ["/mnt/data1", "/mnt/data2"]' "${agent_mounts}"
  jq --exit-status '.[] | select(.target == "/mnt/data1") | .source == "lxd_data1" and .fstype == "virtiofs" and (.options // []) == []' "${agent_mounts}"
  jq --exit-status '.[] | select(.target == "/mnt/data2") | .source == "lxd_data2" and .fstype == "virtiofs" and .options == ["ro"]' "${agent_mounts}"

  sub_test "Test hot-removing a disk regenerates agent-mounts.json"
  lxc config device remove m-disk data2
  jq --exit-status 'map(.target) == ["/mnt/data1"]' "${agent_mounts}"

  lxc delete -f m-disk
  rm -rf "${share}"
}

test_microvm_devices_nic() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  local br_name="lxdm$$"
  lxc network create "${br_name}" ipv4.address=none ipv6.address=none

  sub_test "Test MicroVM bridged NIC device attachment"
  lxc init testimage m-nic --microvm
  lxc config device add m-nic eth0 nic network="${br_name}" name=eth0

  lxc start m-nic
  [ "$(lxc list -f csv -c s m-nic)" = "RUNNING" ]

  sub_test "Test the host side of the NIC is attached to the bridge"
  local host_name
  host_name="$(lxc config get m-nic volatile.eth0.host_name)"
  [ -n "${host_name}" ]
  [ "$(< "/sys/class/net/${host_name}/master/ifindex")" = "$(< "/sys/class/net/${br_name}/ifindex")" ]
  [ -n "$(lxc config get m-nic volatile.eth0.hwaddr)" ]

  sub_test "Test the host side of the NIC is removed on stop"
  lxc stop -f m-nic
  [ ! -e "/sys/class/net/${host_name}" ]

  lxc delete m-nic
  lxc network delete "${br_name}"
}

test_microvm_negative() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  lxc init testimage m-neg --microvm
  lxc start m-neg
  [ "$(lxc list -f csv -c s m-neg)" = "RUNNING" ]

  sub_test "Unsupported operations return errors on running instance"
  _microvm_assert_fails_with "Not supported" lxc pause m-neg
  [ "$(lxc list -f csv -c s m-neg)" = "RUNNING" ]
  _microvm_assert_fails_with "Not supported" lxc snapshot m-neg snap0
  _microvm_assert_fails_with "Stateful stop is not supported for MicroVM instances" lxc stop --stateful m-neg
  [ "$(lxc list -f csv -c s m-neg)" = "RUNNING" ]
  _microvm_assert_fails_with "Not supported" lxc file pull m-neg/etc/hostname -

  lxc stop -f m-neg

  sub_test "Unsupported operations return errors on stopped instance"
  _microvm_assert_fails_with "Not supported" lxc snapshot m-neg snap0
  lxc query /1.0/instances/m-neg/snapshots | jq --exit-status 'length == 0'
  _microvm_assert_fails_with "Not supported" lxc rebuild testimage m-neg
  _microvm_assert_fails_with "Create backup" lxc export m-neg "${TEST_DIR}/m-neg.tar.gz"
  lxc query /1.0/instances/m-neg/backups | jq --exit-status 'length == 0'
  rm -f "${TEST_DIR}/m-neg.tar.gz"

  lxc delete m-neg
}

# Waits up to 60 seconds for a MicroVM to report the given status.
_microvm_wait_status() {
  local name="${1}"
  local status="${2}"

  for _ in $(seq 60); do
    if [ "$(lxc list -f csv -c s "${name}")" = "${status}" ]; then
      return 0
    fi

    sleep 1
  done

  echo "ERROR: ${name} did not reach status ${status}"
  return 1
}

test_microvm_supervisor() {
  if ! _microvm_check_prerequisites; then
    return 0
  fi

  ensure_import_testimage

  local logs="${LXD_DIR}/logs/m-sup"
  local sup_pid vm_pid

  lxc init testimage m-sup --microvm
  lxc start m-sup

  sub_test "Test the VM runs in a child of the forklibkrun supervisor"
  sup_pid="$(< "${logs}/libkrun.pid")"
  vm_pid="$(pgrep -P "${sup_pid}")"
  [ -n "${vm_pid}" ]
  grep -aF -- "--child" "/proc/${vm_pid}/cmdline" >/dev/null
  ! grep -aF -- "--child" "/proc/${sup_pid}/cmdline" || false

  sub_test "Test a VM process crash stops the instance"
  kill -9 "${vm_pid}"
  _microvm_wait_status m-sup STOPPED
  ! kill -0 "${sup_pid}" 2>/dev/null || false
  [ ! -e "${logs}/libkrun.exit" ]

  sub_test "Test killing the supervisor also kills the VM process"
  lxc start m-sup
  sup_pid="$(< "${logs}/libkrun.pid")"
  vm_pid="$(pgrep -P "${sup_pid}")"
  kill -9 "${sup_pid}"
  _microvm_wait_status m-sup STOPPED
  ! kill -0 "${vm_pid}" 2>/dev/null || false

  sub_test "Test force stop terminates both processes"
  lxc start m-sup
  sup_pid="$(< "${logs}/libkrun.pid")"
  vm_pid="$(pgrep -P "${sup_pid}")"
  lxc stop -f m-sup
  ! kill -0 "${sup_pid}" 2>/dev/null || false
  ! kill -0 "${vm_pid}" 2>/dev/null || false

  sub_test "Test guest reboot restarts the instance after a daemon restart"
  lxc start m-sup
  sup_pid="$(< "${logs}/libkrun.pid")"
  kill_go_proc "$(< "${LXD_DIR}/lxd.pid")"
  respawn_lxd "${LXD_DIR}" true
  [ "$(lxc list -f csv -c s m-sup)" = "RUNNING" ]
  [ "$(< "${logs}/libkrun.pid")" = "${sup_pid}" ]

  # testimage has no lxd-agent, so type the reboot into the busybox console shell.
  local new_pid=""
  for _ in $(seq 30); do
    new_pid="$(cat "${logs}/libkrun.pid" 2>/dev/null || true)"
    if [ -n "${new_pid}" ] && [ "${new_pid}" != "${sup_pid}" ]; then
      break
    fi

    printf '\nreboot -f\n' | socat -t 2 - "UNIX-CONNECT:${logs}/libkrun.console" >/dev/null 2>&1 || true
    sleep 2
  done

  [ -n "${new_pid}" ]
  [ "${new_pid}" != "${sup_pid}" ]
  _microvm_wait_status m-sup RUNNING
  ! kill -0 "${sup_pid}" 2>/dev/null || false

  lxc delete -f m-sup
}

test_microvm_basic() {
  test_microvm_lifecycle
}
