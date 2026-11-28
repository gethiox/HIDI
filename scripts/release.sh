#!/usr/bin/env bash
# Builds the files of a HIDI release: an archive for every supported
# system, and a list of their checksums.
#
#   scripts/release.sh VERSION [DIRECTORY]
#
# VERSION is what "hidi version" will say, such as 2.0.0-beta.1.
# DIRECTORY is where the files go; "dist" unless given.
#
# The result depends only on the source and the Go toolchain named in
# go.mod: building the same commit again, here or elsewhere, gives the
# same files, byte for byte. That lets anyone check that a release was
# built from the source it claims. To that end
#   - the binaries carry no build time, path or build ID,
#   - the files in the archives have the time of the commit and no owner,
#   - and they are packed in a fixed order.
#
# The Windows archives also hold Microsoft's Windows.Devices.Midi2.dll,
# which HIDI needs beside hidi.exe until Windows brings it along. It is
# taken from the package packaging/windows/midi-api names, by address and
# checksum; see there. MIDI2_NUPKG names a copy of that package that is
# already at hand, which is checked the same.
#
# And they hold raylib.dll, which draws the window, and libffi-8.dll,
# through which Go calls it, beside hidi.exe. Built as a plain "go build",
# the two libraries carry these files inside the program and write them
# out where it starts: raylib-go into a new directory under %TEMP%, which
# a HIDI that never closes a window leaves behind, and the ffi library
# into %LocalAppData%\github.com\jupiterrider\ffi, and on Windows on ARM
# not at all, so that the program could not start. The tags extlib and
# ffi_no_embed make them load the files beside hidi.exe instead, where
# Windows looks first: HIDI then writes nothing outside its own
# directories. The files are those of the versions go.sum names, taken
# from the module cache unchanged.
#
# It needs go, tar, gzip, zip, unzip, curl and sha256sum, and a clean git
# checkout.
set -euo pipefail
umask 022 # the modes of the packed files must not depend on who builds

version=${1:?usage: scripts/release.sh VERSION [DIRECTORY]}
out=${2:-dist}
cd "$(dirname "$0")/.."

if [ -n "$(git status --porcelain)" ]; then
    echo "release.sh: the checkout has uncommitted changes; a release is built from a commit" >&2
    exit 1
fi

# The time of the commit, for everything that wants a time.
epoch=$(git log -1 --format=%ct)
export SOURCE_DATE_EPOCH=$epoch TZ=UTC LC_ALL=C
export CGO_ENABLED=0 GOFLAGS=-mod=readonly
# The toolchain go.mod names, whatever Go the builder has installed, and
# none of the builder's own choices for the processors built for.
export GOTOOLCHAIN=$(sed -n 's/^toolchain //p' go.mod)
unset GOAMD64 GOARM64 GOEXPERIMENT

rm -rf "$out"
mkdir -p "$out"
out=$(cd "$out" && pwd)
# Archives written into the checkout would make it one with changes for
# every build after the first, and the builds differ by that. A directory
# git ignores, such as dist, does not count.
case "$out/" in
"$PWD"/*)
    if ! git check-ignore -q "$out"; then
        echo "release.sh: $out is inside the checkout and not ignored by git; name a directory outside it, or dist" >&2
        exit 1
    fi
    ;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The icon and the version of the Windows program, as resources "go build"
# links in. They are made here, for the machine that builds, and are not
# part of the checkout: git ignores them.
(
    cd cmd/hidi
    go run ../../internal/assets/icon/render winres
    # Windows wants four numbers; what follows a hyphen is left out there
    # and kept in the version the program shows.
    numbers=${version%%-*}
    go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64,arm64 \
        --product-version "$numbers.0" --file-version "$numbers.0"
)

# Microsoft's MIDI API, for the Windows archives.
midi_url=$(sed -n 's/^url *= *//p' packaging/windows/midi-api)
midi_sum=$(sed -n 's/^sha256 *= *//p' packaging/windows/midi-api)
midi=
if [ -n "$midi_url" ] || [ -n "${MIDI2_NUPKG:-}" ]; then
    package=${MIDI2_NUPKG:-$work/midi2.nupkg}
    if [ -z "${MIDI2_NUPKG:-}" ]; then
        echo "fetching $midi_url"
        curl -fsSL --retry 3 -o "$package" "$midi_url"
    fi
    have=$(sha256sum "$package" | cut -d' ' -f1)
    if [ "$have" != "$midi_sum" ]; then
        echo "release.sh: the MIDI API package has the checksum" >&2
        echo "  $have" >&2
        echo "and packaging/windows/midi-api says ${midi_sum:-nothing}; if the package is the right one, put that checksum there" >&2
        exit 1
    fi
    midi=$work/midi2
    mkdir "$midi"
    unzip -q -o "$package" 'runtimes/win-x64/native/Windows.Devices.Midi2.*' 'runtimes/win-arm64/native/Windows.Devices.Midi2.*' -d "$midi"
else
    echo "release.sh: packaging/windows/midi-api names no package: the Windows archives are built WITHOUT Windows.Devices.Midi2.dll" >&2
fi

# raylib and libffi for the Windows archives, from the modules HIDI is
# built with.
raylib_dir=$(go list -m -f '{{.Dir}}' github.com/gen2brain/raylib-go/raylib)
ffi_dir=$(go list -m -f '{{.Dir}}' github.com/jupiterrider/ffi)

# system/architecture[/ARM version], the name it has in a file name, and
# build tags. The 64-bit Linux programs load libX11 and libffi for their
# window when they start, and do not start without them; those built with
# the tag headless have no window and need no library of the system, for
# machines without a desktop. The 32-bit ARM programs have no window
# anyway.
targets="
linux/amd64      linux-amd64
linux/amd64      linux-amd64-headless    headless
linux/arm64      linux-arm64
linux/arm64      linux-arm64-headless    headless
linux/arm/7      linux-armv7
linux/arm/6      linux-armv6
windows/amd64    windows-amd64           extlib,ffi_no_embed
windows/arm64    windows-arm64           extlib,ffi_no_embed
"

echo "$targets" | while read -r target name buildtags; do
    [ -n "$target" ] || continue
    IFS=/ read -r goos goarch goarm <<<"$target"
    dir="hidi-$version-$name"
    mkdir "$work/$dir"
    exe=hidi
    [ "$goos" = windows ] && exe=hidi.exe

    echo "building $dir"
    # The Windows program is one without a console: a double click opens
    # no black window. Started from a command line it writes there.
    kind= tags=
    [ "$goos" = windows ] && kind="-H windowsgui"
    [ -n "$buildtags" ] && tags="-tags=$buildtags"
    GOOS=$goos GOARCH=$goarch GOARM=$goarm go build -trimpath -buildvcs=true $tags \
        -ldflags "-s -w -buildid= $kind -X github.com/gethiox/HIDI/internal/buildinfo.Version=$version" \
        -o "$work/$dir/$exe" ./cmd/hidi

    # With modes of their own: those of the checkout depend on how it
    # was made.
    install -m 0644 LICENSE README.md "$work/$dir/"
    [ "$goos" = linux ] && install -m 0644 packaging/linux/70-hidi.rules "$work/$dir/"
    # An entry for the desktop's menu, where there is a desktop.
    [ "$goos" = linux ] && [ "$buildtags" != headless ] && install -m 0644 packaging/linux/hidi.desktop "$work/$dir/"
    if [ "$goos" = windows ]; then
        gzip -dc "$raylib_dir/libs/windows_$goarch/raylib.dll.gz" >"$work/$dir/raylib.dll"
        install -m 0644 "$ffi_dir/assets/libffi/windows_$goarch/libffi-8.dll" "$work/$dir/"
        # The notices of raylib, of its Go bindings and of GLFW, which
        # raylib.dll holds: raylib's own is in its header.
        {
            echo "raylib.dll is raylib, with GLFW, as raylib-go ships it."
            echo
            echo "== raylib =="
            sed -n '/LICENSE: zlib/,/source distribution/p' "$raylib_dir/raylib.h" | sed 's/^\* \{0,3\}//'
            echo
            echo "== raylib-go =="
            cat "$raylib_dir/LICENSE"
            echo
            echo "== GLFW =="
            cat "$raylib_dir/external/glfw/LICENSE.md"
        } >"$work/$dir/raylib-LICENSE.txt"
        chmod 0644 "$work/$dir/raylib-LICENSE.txt"
        install -m 0644 "$ffi_dir/assets/libffi/LICENSE" "$work/$dir/libffi-LICENSE.txt"
        chmod 0644 "$work/$dir/raylib.dll"
    fi
    if [ "$goos" = windows ] && [ -n "$midi" ]; then
        from=$midi/runtimes/win-x64/native
        [ "$goarch" = arm64 ] && from=$midi/runtimes/win-arm64/native
        install -m 0644 "$from/Windows.Devices.Midi2.dll" "$from/Windows.Devices.Midi2.pri" "$work/$dir/"
        install -m 0644 packaging/windows/Windows.Devices.Midi2-LICENSE.txt "$work/$dir/"
    fi
    chmod 0755 "$work/$dir" "$work/$dir/$exe"
    find "$work/$dir" -exec touch -d "@$epoch" {} +

    if [ "$goos" = windows ]; then
        # -X leaves out owners and extra times; the names are given in
        # order, since zip keeps the order it is told.
        (cd "$work" && find "$dir" -type f | sort | zip -q -X -@ "$out/$dir.zip")
    else
        tar -C "$work" --sort=name --format=ustar --owner=0 --group=0 --numeric-owner \
            --mtime="@$epoch" -cf - "$dir" | gzip -n -9 >"$out/$dir.tar.gz"
    fi
done

(cd "$out" && sha256sum hidi-* >SHA256SUMS)
echo
cat "$out/SHA256SUMS"
