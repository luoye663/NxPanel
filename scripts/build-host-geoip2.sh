#!/usr/bin/env bash
set -euo pipefail

runtime=$1
package_version=$2
runtime_sha=$3
geoip_sha=$4
mmdb_commit=$5
mmdb_sha=$6
output=$7
expected_binary=${8:-}
export LC_ALL=C
umask 022

. /etc/os-release
case "${ID}:${VERSION_ID}" in
    debian:12|debian:12.*) os=debian:12 ;;
    ubuntu:22.04) test -n "$expected_binary"; os=ubuntu:22.04 ;;
    ubuntu:24.04) os=ubuntu:24.04 ;;
    rocky:9|rocky:9.*) os=rocky:9 ;;
    *) exit 1 ;;
esac
case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64) arch=arm64 ;;
    *) exit 1 ;;
esac
case "$runtime" in
    nginx) binary=/usr/sbin/nginx ;;
    openresty) binary=/usr/local/openresty/nginx/sbin/nginx ;;
    *) exit 1 ;;
esac

work=$(mktemp -d)
master=
cleanup() {
    if [[ -n "$master" ]]; then kill -QUIT "$master" 2>/dev/null || true; wait "$master" || true; fi
    rm -rf "$work"
}
trap cleanup EXIT
cd "$work"

download() {
    curl --proto '=https' --proto-redir '=https' --fail --silent --show-error --location --retry 2 --max-time 120 "$1" -o "$2"
}

if [[ "$os" == rocky:9 ]]; then
    dnf -y --enablerepo=crb install gcc make findutils tar gzip patch curl jq libmaxminddb-devel openssl-devel pcre2-devel zlib-devel dnf-plugins-core
    if [[ "$runtime" == nginx ]]; then
        base='https://nginx.org/packages/centos/9/$basearch/'
        key=https://nginx.org/keys/nginx_signing.key
    else
        base='https://openresty.org/package/centos/9/$basearch/'
        key=https://openresty.org/package/pubkey2.gpg
    fi
    printf '[nxpanel-runtime]\nname=Official %s\nbaseurl=%s\ngpgcheck=1\ngpgkey=%s\nmodule_hotfixes=true\n' \
        "$runtime" "$base" "$key" > /etc/yum.repos.d/nxpanel-runtime.repo
    rpm --import "$key"
    dnf -y download --disablerepo='*' --enablerepo=nxpanel-runtime "$runtime-${package_version#*:}"
    rpmkeys --checksig ./*.rpm
    dnf -y --setopt=localpkg_gpgcheck=1 install ./*.rpm
    actual=$(rpm -qf --qf '%{EPOCHNUM}:%{VERSION}-%{RELEASE}' "$binary")
else
    export DEBIAN_FRONTEND=noninteractive
    apt_options=(-o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 -o Acquire::Retries=1)
    printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d
    chmod 755 /usr/sbin/policy-rc.d
    apt-get "${apt_options[@]}" update -q -o APT::Update::Error-Mode=any
    apt-get "${apt_options[@]}" install -y --no-install-recommends build-essential ca-certificates curl gnupg jq libmaxminddb-dev libssl-dev libpcre2-dev zlib1g-dev
    if [[ "$runtime" == nginx ]]; then
        base="https://nginx.org/packages/$ID"
        key=https://nginx.org/keys/nginx_signing.key
        component=nginx
    else
        base="https://openresty.org/package/$ID"
        key=https://openresty.org/package/pubkey.gpg
        component=main
        if [[ "$ID" == debian ]]; then component=openresty; fi
    fi
    download "$key" upstream-key
    gpg --batch --dearmor -o /usr/share/keyrings/nxpanel-runtime.gpg upstream-key
    source=/etc/apt/sources.list.d/nxpanel-runtime.list
    printf 'deb [signed-by=/usr/share/keyrings/nxpanel-runtime.gpg] %s %s %s\n' "$base" "$VERSION_CODENAME" "$component" > "$source"
    only_upstream=(-o "Dir::Etc::sourcelist=$source" -o Dir::Etc::sourceparts=-)
    apt-get "${apt_options[@]}" "${only_upstream[@]}" update -q -o APT::Update::Error-Mode=any
    apt-get "${apt_options[@]}" "${only_upstream[@]}" download "$runtime=$package_version"
    apt-get "${apt_options[@]}" update -q -o APT::Update::Error-Mode=any
    apt-get "${apt_options[@]}" install -y --no-install-recommends ./*.deb
    actual=$(dpkg-query -W -f='${Version}' "$runtime")
fi
test "$actual" = "$package_version"
digest=$(sha256sum "$binary" | cut -d ' ' -f 1)
if [[ -n "$expected_binary" && "$digest" != "$expected_binary" ]]; then
    printf 'The official package binary does not match the test host (actual %s, expected %s).\n' "$digest" "$expected_binary" >&2
    exit 1
fi

version_output=$("$binary" -V 2>&1)
version=$(printf '%s\n' "$version_output" | sed -n "s|^nginx version: $runtime/||p")
[[ "$version" =~ ^[0-9]+(\.[0-9]+){2,3}$ ]]
if [[ "$runtime" == nginx ]]; then
    download "https://nginx.org/download/nginx-$version.tar.gz" runtime.tar.gz
else
    download "https://openresty.org/download/openresty-$version.tar.gz" runtime.tar.gz
fi
download https://codeload.github.com/leev/ngx_http_geoip2_module/tar.gz/refs/tags/3.4 geoip2.tar.gz
download "https://raw.githubusercontent.com/maxmind/MaxMind-DB/$mmdb_commit/test-data/GeoIP2-Country-Test.mmdb" test.mmdb
printf '%s  %s\n' "$runtime_sha" runtime.tar.gz "$geoip_sha" geoip2.tar.gz "$mmdb_sha" test.mmdb | sha256sum -c -
tar -xzf runtime.tar.gz
tar -xzf geoip2.tar.gz
if [[ "$runtime" == nginx ]]; then
    cd "nginx-$version"
else
    nginx_sources=()
    for source_dir in "openresty-$version"/bundle/nginx-[0-9]*; do
        if [[ -x "$source_dir/configure" && -d "$source_dir/auto" ]]; then
            nginx_sources+=("$source_dir")
        fi
    done
    if [[ "${#nginx_sources[@]}" != 1 ]]; then
        printf 'Could not uniquely identify the Nginx source directory in OpenResty %s.\n' "$version" >&2
        exit 1
    fi
    cd "${nginx_sources[0]}"
fi

args=()
options=$(printf '%s\n' "$version_output" | sed -n 's/^configure arguments: //p')
for option in $options; do
    case "$option" in
        --with-compat|--with-debug|--with-threads|--with-file-aio|--with-http_ssl_module|--with-http_v2_module|--with-http_v3_module|--with-http_realip_module|--with-http_dav_module|--with-http_gzip_static_module|--with-http_gunzip_module|--without-http_gzip_module|--without-http_proxy_module|--without-http_fastcgi_module|--without-http_scgi_module|--without-http_uwsgi_module|--without-http_memcached_module|--without-http-cache|--without-http_upstream_zone_module|--without-pcre)
            args+=("$option") ;;
    esac
done
if [[ -n "$expected_binary" && -d /nxpanel-module-smoke ]]; then
    args+=(--add-dynamic-module=/nxpanel-module-smoke)
fi
./configure "${args[@]}" --add-dynamic-module="$work/ngx_http_geoip2_module-3.4"
make -j "$(getconf _NPROCESSORS_ONLN)" modules
module="$work/ngx_http_geoip2_module.so"
install -m 644 objs/ngx_http_geoip2_module.so "$module"
if [[ -n "$expected_binary" && -d /nxpanel-module-smoke ]]; then
    install -m 644 objs/ngx_http_nxpanel_fixture_module.so "$output/ngx_http_nxpanel_fixture_module.so"
fi

cd "$work"
mkdir logs
conf="$work/nginx.conf"
sockets="$work/http.sock"
cat > "$conf" <<EOF
worker_processes 1;
pid $work/nginx.pid;
error_log stderr;
events {}
http { access_log off; server { listen unix:$sockets; return 200 before; } }
EOF
"$binary" -t -e stderr -p "$work/" -c "$conf"
"$binary" -e stderr -p "$work/" -c "$conf" -g 'daemon off;' &
master=$!
request() { curl --fail --silent --max-time 1 --unix-socket "$sockets" http://localhost/; }
wait_for() {
    local expected=$1
    for _ in {1..100}; do
        if [[ "$(request)" == "$expected" ]]; then return 0; fi
        kill -0 "$master" || return 1
        sleep 0.1
    done
    return 1
}
wait_for before
old_workers=$(cat "/proc/$master/task/$master/children")
test -n "$old_workers"
cat > "$conf" <<EOF
load_module $module;
worker_processes 1;
pid $work/nginx.pid;
error_log stderr;
events {}
http {
    access_log off;
    map \$host \$test_ip { default 81.2.69.142; }
    geoip2 $work/test.mmdb { \$test_country source=\$test_ip country iso_code; }
    server { listen unix:$sockets; return 200 \$test_country; }
}
EOF
"$binary" -t -e stderr -p "$work/" -c "$conf"
"$binary" -e stderr -p "$work/" -c "$conf" -s reload
wait_for GB
new_workers=$(cat "/proc/$master/task/$master/children")
test -n "$new_workers" && test "$new_workers" != "$old_workers"

install -m 644 "$module" "$output/ngx_http_geoip2_module.so"
jq -n --arg os "$os" --arg arch "$arch" --arg runtime "$runtime" --arg version "$version" --arg digest "$digest" \
    '{os:$os,arch:$arch,runtime:$runtime,runtime_version:$version,binary_sha256:$digest}' > "$output/target.json"
