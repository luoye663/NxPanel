#!/usr/bin/env bash
set -euo pipefail

go_bin=${GO:-go}
binary=$(realpath -e "${1:?Supply an absolute Nginx binary path}")
[[ "$1" == /* && -x "$binary" ]]
. /etc/os-release
case "${ID}:${VERSION_ID}" in
    debian:12|debian:12.*) os=debian:12; image=debian:12 ;;
    ubuntu:22.04|ubuntu:24.04) os="ubuntu:$VERSION_ID"; image=$os ;;
    rocky:9|rocky:9.*) os=rocky:9; image=rockylinux:9 ;;
    *) printf 'Unsupported local test OS: %s:%s\n' "$ID" "$VERSION_ID" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64) arch=arm64 ;;
    *) printf 'Unsupported local test architecture\n' >&2; exit 1 ;;
esac
release=$("$binary" -V 2>&1 | sed -n 's/^nginx version: //p')
[[ "$release" =~ ^(nginx|openresty)/[0-9]+(\.[0-9]+){2,3}$ ]]
runtime=${release%/*}
if [[ "$os" == rocky:9 ]]; then
    info=$(rpm -qf --qf '%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n' "$binary")
    IFS=$'\t' read -r owner package_version <<< "$info"
    test "$owner" = "$runtime"
else
    info=$(dpkg-query -S "$binary")
    [[ "$info" == "$runtime: $binary" || "$info" == "$runtime:$arch: $binary" ]]
    package_version=$(dpkg-query -W -f='${Version}' "$runtime")
fi
[[ "$package_version" =~ ^[A-Za-z0-9][A-Za-z0-9.+:~_-]{0,127}$ ]]
digest=$(sha256sum "$binary" | cut -d ' ' -f 1)

runtime_sha=${MODULE_RUNTIME_SHA256:-}
if [[ -z "$runtime_sha" && "$release" == openresty/1.31.1.1 ]]; then
    runtime_sha=65b78baadd3f0984055de89bf13f4a1932e5bfe9c31932037a134ea2b1a0ce42
fi
geoip_sha=ad72fc23348d715a330994984531fab9b3606e160483236737f9a4a6957d9452
mmdb_commit=c454a21f31fd17f2843f16094e9afb3a14a42078
mmdb_sha=fc12d6470b35b70bd3e4534dd433d3e262a6c73f7682f85c344e832d4190f02b
if [[ ! "$runtime_sha" =~ ^[a-f0-9]{64}$ ]]; then
    printf 'Set MODULE_RUNTIME_SHA256 to the verified official source archive checksum for %s.\n' "$release" >&2
    exit 1
fi

base=tmp/module-fixture
mkdir -p "$base"
work=$(mktemp -d "$base/linux.XXXXXX")
mkdir -p "$work/builds/geoip2/host"
bash scripts/nginx-module-smoke/package-fixture-scripts.sh "$work/builds"
printf 'Building %s %s (%s, %s) in Docker; host packages will not be changed.\n' "$runtime" "$package_version" "$os" "$arch"
proxy=()
if [[ "${MODULE_BUILD_USE_PROXY:-0}" == 1 ]]; then
    export http_proxy=${http_proxy:-${HTTP_PROXY:-}} https_proxy=${https_proxy:-${HTTPS_PROXY:-}} no_proxy=${no_proxy:-${NO_PROXY:-}}
    proxy=(--env http_proxy --env https_proxy --env no_proxy)
fi
docker run --rm --platform "linux/$arch" "${proxy[@]}" \
    --mount "type=bind,source=$PWD/scripts/build-host-geoip2.sh,target=/build-host-geoip2.sh,readonly" \
    --mount "type=bind,source=$PWD/scripts/nginx-module-smoke,target=/nxpanel-module-smoke,readonly" \
    --mount "type=bind,source=$PWD/$work/builds/geoip2/host,target=/out" "$image" \
    bash /build-host-geoip2.sh "$runtime" "$package_version" "$runtime_sha" "$geoip_sha" "$mmdb_commit" "$mmdb_sha" /out "$digest"
mkdir -p "$work/builds/fixture-http/host"
mv "$work/builds/geoip2/host/ngx_http_nxpanel_fixture_module.so" "$work/builds/fixture-http/host/"
cp "$work/builds/geoip2/host/target.json" "$work/builds/fixture-http/host/"
"$go_bin" run ./cmd/nxpanel-module-catalog -test-fixture -metadata scripts/nginx-module-smoke/catalog.json \
    -sequence "$(date +%s%N)" -input "$work/builds" -output "$work/fixture"
printf 'Local feed: %s/fixture/feed\nPublic key: %s/fixture/trust.pub\n' "$work" "$work"
