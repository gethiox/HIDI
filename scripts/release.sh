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
# It needs go, tar, gzip, zip and sha256sum, and a clean git checkout.
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

rm -rf "$out"
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# system/architecture[/ARM version], and the name it has in a file name.
targets="
linux/amd64      linux-amd64
linux/arm64      linux-arm64
linux/arm/7      linux-armv7
linux/arm/6      linux-armv6
windows/amd64    windows-amd64
windows/arm64    windows-arm64
"

echo "$targets" | while read -r target name; do
    [ -n "$target" ] || continue
    IFS=/ read -r goos goarch goarm <<<"$target"
    dir="hidi-$version-$name"
    mkdir "$work/$dir"
    exe=hidi
    [ "$goos" = windows ] && exe=hidi.exe

    echo "building $dir"
    GOOS=$goos GOARCH=$goarch GOARM=$goarm go build -trimpath -buildvcs=true \
        -ldflags "-s -w -buildid= -X github.com/gethiox/HIDI/internal/buildinfo.Version=$version" \
        -o "$work/$dir/$exe" ./cmd/hidi

    cp LICENSE README.md "$work/$dir/"
    [ "$goos" = linux ] && cp packaging/linux/70-hidi.rules packaging/linux/hidi.desktop "$work/$dir/"
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
