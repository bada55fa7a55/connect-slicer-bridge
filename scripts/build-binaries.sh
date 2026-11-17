#!/bin/sh
set -eu

out=${1:?usage: build-binaries.sh <output dir>}
mkdir -p "$out"

for target in linux/amd64 linux/arm64 linux/arm/7 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    os=${target%%/*}
    rest=${target#*/}
    arch=${rest%%/*}
    arm=
    [ "$arch" = arm ] && arm=${rest#*/}

    name=connect-slicer-bridge_${os}_${arch}${arm:+v$arm}
    [ "$os" = windows ] && name=$name.exe

    CGO_ENABLED=0 GOOS=$os GOARCH=$arch GOARM=$arm \
        go build -trimpath -ldflags="-s -w" -o "$out/$name" .
done

(cd "$out" && sha256sum connect-slicer-bridge_* > SHA256SUMS)
