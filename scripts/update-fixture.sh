#!/usr/bin/env bash
set -euo pipefail

go_bin=${GO:-go}
arch=$($go_bin env GOARCH)
case "$arch" in amd64|arm64) ;; *) printf 'Unsupported test architecture: %s\n' "$arch" >&2; exit 1 ;; esac
base=tmp/update-fixture
mkdir -p "$base/feed" "$base/baseline"
work=$(mktemp -d "$base/work.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

"$go_bin" build -o "$work/package" ./cmd/nxpanel-update-package
"$work/package" -keygen "$work/keys" -key-id fixture
cp "$work/keys/trust.pub" "$base/trust.pub"
export NXPANEL_UPDATE_TRUST_KEYS
NXPANEL_UPDATE_TRUST_KEYS=$(cat "$base/trust.pub")

make build GO="$go_bin" BUILD_TAGS=embed_web,update_dev VERSION=v0.0.1 \
    BINARY_API="$base/baseline/nxpanel-api" BINARY_AGENT="$base/baseline/nxpanel-agent" BINARY_UPDATER="$base/baseline/nxpanel-updater"

for version in v0.0.2 v0.0.3-rc.1 v0.0.4-rc.1; do
    make build-api build-agent GO="$go_bin" BUILD_TAGS=embed_web,update_dev VERSION="$version" \
        BINARY_API="$work/nxpanel-api" BINARY_AGENT="$work/nxpanel-agent"
    NXPANEL_UPDATE_SIGNING_KEYS=$(cat "$work/keys/signing.key") \
        "$work/package" -version "$version" "-$arch" "$work" -out "$base/feed/$version"
done

printf 'tampered\n' >> "$base/feed/v0.0.4-rc.1/nxpanel-update-linux-$arch.tar.gz"

cat > "$base/feed/index.json" <<'EOF'
[
  {"tag_name":"v0.0.2","prerelease":false,"body":"Fixture: stable update"},
  {"tag_name":"v0.0.3-rc.1","prerelease":true,"body":"Fixture: preview update"}
]
EOF
cat > "$base/feed/corrupt.json" <<'EOF'
[
  {"tag_name":"v0.0.4-rc.1","prerelease":true,"body":"Fixture: damaged package"}
]
EOF
printf 'Feed: %s/feed; test public key: %s/trust.pub\n' "$base" "$base"
