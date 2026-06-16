#!/usr/bin/env bash
# Install crypt from the latest GitHub release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/veertuinc/crypt/edge/scripts/install.sh | bash
#
# Environment:
#   INSTALL_DIR  install destination (default: /usr/local/bin)

set -euo pipefail

repo="veertuinc/crypt"
release_base="https://github.com/${repo}/releases/latest/download"

if [[ "$(uname -s)" != Darwin ]]; then
	echo "crypt requires macOS" >&2
	exit 1
fi

case "$(uname -m)" in
arm64 | aarch64) goarch=arm64 ;;
x86_64) goarch=amd64 ;;
*)
	echo "unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

asset="crypt_darwin_${goarch}.tar.gz"
install_dir="${INSTALL_DIR:-/usr/local/bin}"
tmp_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmp_dir"
}
trap cleanup EXIT

curl -fsSL "${release_base}/checksums.txt" -o "${tmp_dir}/checksums.txt"
curl -fsSL "${release_base}/${asset}" -o "${tmp_dir}/${asset}"

(
	cd "$tmp_dir"
	grep -F " ${asset}" checksums.txt | shasum -a 256 -c -
)

tar -xzf "${tmp_dir}/${asset}" -C "$tmp_dir"

if [[ -w "$install_dir" ]]; then
	install -m 755 "${tmp_dir}/crypt" "${install_dir}/crypt"
else
	sudo install -m 755 "${tmp_dir}/crypt" "${install_dir}/crypt"
fi

echo "Installed crypt to ${install_dir}/crypt"
