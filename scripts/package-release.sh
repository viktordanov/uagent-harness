#!/usr/bin/env bash
# Build and package the release archives for one version.
#
# Each archive holds one versioned directory with uah, LICENSE, NOTICE, and
# THIRD_PARTY_NOTICES.md, and has a .sha256 sidecar. The layout follows
# rs-memoria's release archives, which the Homebrew tap and the AUR package
# already read.
#
# Every archive field that a timestamp, a user name, or a file order could
# change is set explicitly, so two runs over the same commit produce the same
# bytes.
#
# UAH_SOURCE builds another checkout, such as an older tag, with this script.
#
# Usage:
#   scripts/package-release.sh --version 1.0.1 --output dist
set -euo pipefail
version=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) version="${2:?--version needs a value}"; shift 2 ;;
    --output) output="${2:?--output needs a value}"; shift 2 ;;
    -h|--help) sed -n '2,16p' "$0"; exit 0 ;;
    *) echo "package-release: unknown argument $1" >&2; exit 2 ;;
  esac
done
if [ -z "$version" ] || [ -z "$output" ]; then
  echo "package-release: --version and --output are required" >&2
  exit 2
fi
case "$version" in
  v*) echo "package-release: --version takes 1.0.1, not v1.0.1" >&2; exit 2 ;;
esac

root="$(cd "${UAH_SOURCE:-$(dirname "$0")/..}" && pwd)"
# The commit timestamp is the one clock the archives use.
if [ -z "${SOURCE_DATE_EPOCH:-}" ]; then
  SOURCE_DATE_EPOCH="$(git -C "$root" show -s --format=%ct HEAD)"
fi
export SOURCE_DATE_EPOCH

# GNU tar: macOS needs gtar (brew install gnu-tar).
tar_bin=tar
if command -v gtar >/dev/null; then tar_bin=gtar; fi
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

mkdir -p "$output"
output="$(cd "$output" && pwd)"
staging="$(mktemp -d "${output}/.stage-XXXXXX")"
trap 'rm -rf "$staging"' EXIT

# target  GOOS  GOARCH  file(1) pattern
targets=(
  "aarch64-apple-darwin darwin arm64 Mach-O 64-bit.*arm64"
  "x86_64-apple-darwin darwin amd64 Mach-O 64-bit.*x86_64"
  "aarch64-unknown-linux-gnu linux arm64 ELF 64-bit.*aarch64"
  "x86_64-unknown-linux-gnu linux amd64 ELF 64-bit.*x86-64"
)
for entry in "${targets[@]}"; do
  read -r target goos goarch pattern <<<"$entry"
  prefix="uah-${version}-${target}"
  asset="${prefix}.tar.gz"
  dir="$staging/$prefix"
  install -d -m 755 "$dir"

  # uah is pure Go (modernc.org/sqlite), so CGO stays off and every target
  # cross-compiles from one machine.
  (cd "$root" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=v${version}" \
    -o "$dir/uah" ./cmd/uah)
  file "$dir/uah" | grep -Eq "$pattern" || {
    echo "package-release: $target built $(file -b "$dir/uah")" >&2
    exit 1
  }
  chmod 755 "$dir/uah"
  for doc in LICENSE NOTICE THIRD_PARTY_NOTICES.md; do
    install -m 644 "$root/$doc" "$dir/$doc"
  done

  # --sort=name fixes the member order, --mtime fixes every timestamp, the
  # owner options remove the build account, and `gzip -n` removes the
  # compressor's own name and timestamp.
  "$tar_bin" \
    --format=gnu \
    --sort=name \
    --mtime="@${SOURCE_DATE_EPOCH}" \
    --owner=0 --group=0 --numeric-owner \
    -C "$staging" -cf - "$prefix" | gzip -n -9 >"$output/$asset"
  (cd "$output" && sha256 "$asset" >"${asset}.sha256")
  echo "$output/$asset"
done
