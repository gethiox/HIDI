#!/bin/sh
# Runs the tests that need the kernel's input and MIDI interfaces (uinput
# and the ALSA sequencer) in a virtual machine, for machines and containers
# that do not have them. It boots an installed generic kernel with the host's
# file system, so nothing is copied or downloaded.
#
# Needs: virtme-ng, qemu-system-x86, a kernel in /boot with its modules
# (linux-image-generic), udev, zstd, and util-linux's script. On Ubuntu:
#
#   apt-get install virtme-ng qemu-system-x86 linux-image-generic udev zstd
#
# Usage: scripts/vm-test.sh [go test flags, such as -test.run TestName]
set -eu

cd "$(dirname "$0")/.."
kernel=$(ls /boot/vmlinuz-*-generic 2>/dev/null | sort -V | tail -n 1)
if [ -z "$kernel" ]; then
	echo "vm-test: no generic kernel in /boot; install linux-image-generic" >&2
	exit 1
fi

# The guest sees the host's files read-only and gets a fresh /tmp, so the
# binaries and the results live in a directory that is shared for writing.
work=${HIDI_VM_DIR:-/var/tmp/hidi-vm-test}
mkdir -p "$work"
for pkg in output/alsaseq input/evdev e2e; do
	go test -c -o "$work/$(basename "$pkg").test" "./internal/$pkg"
done
go build -o "$work/hidi" ./cmd/hidi

cat > "$work/run.sh" <<SCRIPT
#!/bin/sh
exec > "$work/output.log" 2>&1
status=0
modprobe snd-seq || status=1
modprobe uinput 2>/dev/null
for suite in alsaseq evdev e2e; do
	echo "=== \$suite"
	HIDI_REQUIRE_ALSA=1 HIDI_REQUIRE_UINPUT=1 HIDI_E2E_BINARY="$work/hidi" \\
		"$work/\$suite.test" $* || status=1
done
echo "tests exited with status \$status"
SCRIPT
chmod +x "$work/run.sh"
rm -f "$work/output.log"

# Without /dev/kvm the machine is emulated, which is slower but works.
kvm=
[ -w /dev/kvm ] || kvm=--disable-kvm
# virtme-ng wants a terminal; script provides one where there is none.
script -qec "vng --disable-microvm $kvm --cpus 2 --memory 2G --rwdir=$work --run $kernel -- $work/run.sh" /dev/null > "$work/vm.log" 2>&1 || true

if [ ! -f "$work/output.log" ]; then
	echo "vm-test: the virtual machine did not run the tests; see $work/vm.log" >&2
	exit 1
fi
cat "$work/output.log"
grep -q 'tests exited with status 0' "$work/output.log"
