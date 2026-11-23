#!/usr/bin/env bash
set -euo pipefail

go_bin=${GO:-go}
arch=$($go_bin env GOARCH)
case "$arch" in amd64|arm64) ;; *) printf 'Unsupported test architecture: %s\n' "$arch" >&2; exit 1 ;; esac
base=${NXPANEL_UPDATE_FIXTURE_DIR:-tmp/update-fixture}
mkdir -p "$base/feed" "$base/baseline"
work=$(mktemp -d "$base/work.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

"$go_bin" build -o "$work/package" ./cmd/nxpanel-update-package
"$go_bin" build -o "$work/trust" ./cmd/nxpanel-trust
"$work/package" -keygen "$work/root" -key-id fixture-root
"$work/package" -keygen "$work/keys" -key-id fixture-updates
cp "$work/root/trust.pub" "$base/trust.pub"
export NXPANEL_ROOT_PUBLIC_KEY
NXPANEL_ROOT_PUBLIC_KEY=$(cat "$base/trust.pub")
jq -n --arg issued "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" --arg expiry "$(date -u -d '+7 days' +'%Y-%m-%dT%H:%M:%SZ')" \
    --arg keys "$(cat "$work/keys/trust.pub")" '{version:1,sequence:1,issued_at:$issued,expires_at:$expiry,update_keys:$keys,build_keys:"",modules:[]}' > "$work/policy.json"
"$work/trust" -sign "$work/policy.json" -key "$work/root/signing.key" -out "$work/policy"
cp "$work/policy/"{trust.json,trust.sig} "$base/feed/"

make build GO="$go_bin" BUILD_TAGS=embed_web,update_dev VERSION=v0.0.1 \
    BINARY_API="$base/baseline/nxpanel-api" BINARY_AGENT="$base/baseline/nxpanel-agent" BINARY_UPDATER="$base/baseline/nxpanel-updater"

for version in v0.0.2 v0.0.3-rc.1 v0.0.4-rc.1; do
    make build-api build-agent GO="$go_bin" BUILD_TAGS=embed_web,update_dev VERSION="$version" \
        BINARY_API="$work/nxpanel-api" BINARY_AGENT="$work/nxpanel-agent"
    NXPANEL_UPDATE_SIGNING_KEYS=$(cat "$work/keys/signing.key") \
        "$work/package" -trust-dir "$work/policy" -version "$version" "-$arch" "$work" -out "$base/feed/$version"
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
