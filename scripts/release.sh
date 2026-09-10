#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C
export LANG=C

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: scripts/release.sh <vX.Y.Z> [s3-bucket]" >&2
  exit 2
fi

probe_version="$1"
bucket="${2:-tsh-runtime-artifacts}"
prefix="tutti-network-probe"

if [[ ! "$probe_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must match vX.Y.Z" >&2
  exit 2
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT

if aws s3api head-object --bucket "$bucket" --key "$prefix/releases/$probe_version/SHA256SUMS" >/dev/null 2>&1; then
  echo "release $probe_version already exists in s3://$bucket/$prefix/releases" >&2
  exit 1
fi

cd "$repository_root"
go test ./...

archive_name="tutti-network-probe-darwin-arm64.tar.gz"
build_dir="$temporary_dir/darwin-arm64"
mkdir -p "$build_dir"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$probe_version" \
  -o "$build_dir/tutti-network-probe" .
tar -C "$build_dir" -czf "$temporary_dir/$archive_name" tutti-network-probe

(
  cd "$temporary_dir"
  shasum -a 256 tutti-network-probe-darwin-arm64.tar.gz >SHA256SUMS
)
sed "s/@VERSION@/$probe_version/g" scripts/install.sh.tmpl >"$temporary_dir/install.sh"
cp scripts/uninstall.sh "$temporary_dir/uninstall.sh"
printf '%s\n' "$probe_version" >"$temporary_dir/latest-version"

release_destination="s3://$bucket/$prefix/releases/$probe_version"
immutable_cache="public, max-age=31536000, immutable"
aws s3 cp "$temporary_dir/tutti-network-probe-darwin-arm64.tar.gz" "$release_destination/tutti-network-probe-darwin-arm64.tar.gz" --content-type application/gzip --cache-control "$immutable_cache"
aws s3 cp "$temporary_dir/SHA256SUMS" "$release_destination/SHA256SUMS" --content-type text/plain --cache-control "$immutable_cache"
aws s3 cp "$temporary_dir/install.sh" "$release_destination/install.sh" --content-type text/x-shellscript --cache-control "$immutable_cache"
aws s3 cp "$temporary_dir/uninstall.sh" "$release_destination/uninstall.sh" --content-type text/x-shellscript --cache-control "$immutable_cache"

stable_destination="s3://$bucket/$prefix"
aws s3 cp "$temporary_dir/install.sh" "$stable_destination/install.sh" --content-type text/x-shellscript --cache-control "public, max-age=60"
aws s3 cp "$temporary_dir/uninstall.sh" "$stable_destination/uninstall.sh" --content-type text/x-shellscript --cache-control "public, max-age=60"
aws s3 cp "$temporary_dir/latest-version" "$stable_destination/latest-version" --content-type text/plain --cache-control "public, max-age=60"

echo "Published $probe_version to $release_destination"
