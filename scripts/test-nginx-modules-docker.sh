#!/usr/bin/env bash
set -euo pipefail

go_bin=${GO:-go}
image=${1:-nxpanel:module-dev}
artifacts=$(realpath -e "${2:?Supply the external Alpine build directory with GeoIP2 and fixture-http}")
fixture_mode=${3:-}
[[ -z "$fixture_mode" || "$fixture_mode" == --manual ]]
image=$(docker image inspect "$image" --format '{{.Id}}')
nonce=$(openssl rand -hex 6)
network=nxpanel-module-test-$nonce
panel=$network-panel
feed=$network-feed
replacement=$network-replacement
mkdir -p tmp/module-fixture
work=$(mktemp -d "$PWD/tmp/module-fixture/docker.XXXXXX")
cookie=$work/cookie

cleanup() {
    local status=$?
    if [[ "$status" != 0 ]]; then docker logs --tail 80 "$panel" >&2 || true; fi
    docker rm -fv "$panel" "$feed" "$replacement" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
    rm -rf -- "$work"
}
trap cleanup EXIT

mkdir -p "$work/builds/geoip2/docker" "$work/builds/fixture-http/docker" "$work/config"
bash scripts/nginx-module-smoke/package-fixture-scripts.sh "$work/builds"
cp "$artifacts/ngx_http_geoip2_module.so" "$work/builds/geoip2/docker/"
cp "$artifacts/ngx_http_nxpanel_fixture_module.so" "$work/builds/fixture-http/docker/"
target=$(docker run --rm --entrypoint /bin/sh "$image" -ec '
    test ! -e /usr/local/nxpanel/modules/ngx_http_geoip2_module.so
    test ! -e /usr/local/nxpanel/modules/ngx_http_nxpanel_fixture_module.so
    if apk info -e libmaxminddb >/dev/null; then exit 1; fi
    case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; *) exit 1 ;; esac
    release=$("$NXPANEL_NGINX_BIN" -V 2>&1 | sed -n "s/^nginx version: //p")
    . /etc/os-release
    printf "%s\t%s\t%s\t%s\t%s\n" "$arch" "${release%/*}" "${release#*/}" "$(sha256sum "$NXPANEL_NGINX_BIN" | cut -d " " -f 1)" "$ID:$(printf "%s\n" "$VERSION_ID" | cut -d . -f 1,2)"
')
IFS=$'\t' read -r arch runtime version digest os <<< "$target"
jq -n --arg arch "$arch" --arg runtime "$runtime" --arg digest "$digest" --arg version "$version" --arg os "$os" \
    '{os:$os,arch:$arch,runtime:$runtime,runtime_version:$version,binary_sha256:$digest}' > "$work/builds/geoip2/docker/target.json"
if ! jq -e --slurpfile expected "$work/builds/geoip2/docker/target.json" '. == $expected[0]' "$artifacts/target.json" >/dev/null; then
    printf 'External module build does not match the running image.\n' >&2
    exit 1
fi
cp "$work/builds/geoip2/docker/target.json" "$work/builds/fixture-http/docker/"
"$go_bin" run ./cmd/nxpanel-module-catalog -test-fixture -metadata scripts/nginx-module-smoke/catalog.json \
    -sequence "$(date +%s)" -input "$work/builds" -output "$work/fixture"

docker network create "$network" >/dev/null
docker run -d --name "$feed" --network "$network" --network-alias module-feed --entrypoint /bin/sh \
    --mount "type=bind,source=$work/fixture/feed,target=/test-feed,readonly" "$image" -ec '
    printf "pid /tmp/feed.pid; error_log stderr; events {} http { access_log /dev/stdout; server { listen 80; root /test-feed; } }" > /tmp/feed.conf
    exec "$NXPANEL_NGINX_BIN" -e stderr -c /tmp/feed.conf -g "daemon off;"
' >/dev/null
docker run -d --name "$panel" --network "$network" -p 127.0.0.1::18888 \
    --mount "type=bind,source=$work/config,target=/opt/nxpanel/nginx" \
    -e "NXPANEL_API_LOGIN_PATH=/modules-$nonce" -e "NXPANEL_AGENT_TOKEN=$(openssl rand -hex 32)" \
    -e NXPANEL_MODULE_TEST_FEED=http://module-feed/modules.json \
    -e "NXPANEL_MODULE_TEST_KEYS=$(cat "$work/fixture/trust.pub")" "$image" >/dev/null

base=https://$(docker port "$panel" 18888/tcp)
api=$base/api/v1/modules-$nonce
ready_url=$api/auth/captcha-config
if [[ "$fixture_mode" == --manual ]]; then ready_url=$base/modules-$nonce; fi
ready=false
for attempt in {1..60}; do
    if curl -ksSf --max-time 2 "$ready_url" >/dev/null 2>&1; then ready=true; break; fi
    sleep 1
done
if ! $ready; then printf 'Test panel did not start.\n' >&2; exit 1; fi
if [[ "$fixture_mode" == --manual ]]; then
    printf 'Open %s/modules-%s, initialize nxPanel, then open Nginx → Modules. Press Enter to clean up.\n' "$base" "$nonce"
    read -r
    exit 0
fi

credentials=$(jq -cn --arg password "Nx!$(openssl rand -hex 20)" '{username:"admin",password:$password}')
curl -ksSf --max-time 5 -H 'Content-Type: application/json' --data "$credentials" "$api/setup/admin" | jq -e .success >/dev/null
curl -ksSf --max-time 5 -c "$cookie" -H 'Content-Type: application/json' --data "$credentials" "$api/auth/login" | jq -e .success >/dev/null
csrf=$(awk '$6 == "csrf-token" { print $7 }' "$cookie")
test -n "$csrf"

list() { curl -ksSf --max-time 26 -b "$cookie" "$api/nginx/modules${1:-}"; }
available() {
    local id=${1:-geoip2} requested=${2:-} result tag ticket
    for attempt in {1..25}; do
        if result=$(list) && jq -e --arg id "$id" '.data | select(.test_feed and .feed_state == "online") | .modules[] | select(.id == $id and .state == "available" and .action == "install")' <<< "$result" >/dev/null &&
            tag=$(curl -ksSf --max-time 26 -b "$cookie" "$api/nginx/modules/releases?id=$id&page=1" | \
                jq -er --arg requested "$requested" '.data | if $requested != "" then .tags[] | select(. == $requested) else .latest end') &&
            ticket=$(curl -ksSf --max-time 26 -b "$cookie" "$api/nginx/modules/preflight?id=$id&tag=$tag" | jq -ce --arg id "$id" '.data | select(.confirmation) | {id:$id,tag,confirmation}'); then
            printf '%s\n' "$ticket"
            return
        fi
        sleep 1
    done
    printf 'Test module is not installable: %s\n' "$result" >&2
    return 1
}
change_module() {
    local action=$1 request=$2 job result
    job=$(curl -ksSf --max-time 8 -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
        --data "$request" "$api/nginx/modules/$action" | jq -er '.data.id')
    for attempt in {1..370}; do
        result=$(curl -ksSf --max-time 5 -b "$cookie" "$api/nginx/modules/jobs/$job")
        if jq -e '.data.state != "pending"' <<< "$result" >/dev/null; then
            jq -c .data <<< "$result"
            return
        fi
        sleep 1
    done
    printf 'Module job did not finish: %s\n' "$job" >&2
    return 1
}
main_hash() { docker exec "$panel" sha256sum /opt/nxpanel/nginx/nginx.conf; }
workers() { docker exec "$panel" cat "/proc/$master/task/$master/children"; }
expect() {
    local response
    response=$(cat)
    if ! jq -e "$1" <<< "$response" >/dev/null; then
        printf 'Unexpected response (%s): %s\n' "$1" "$response" >&2
        return 1
    fi
}

list '?refresh=1' | expect '.data.feed_state == "online" and ([.data.modules[].id] | sort == ["fixture-http","geoip2"])'
feed_log=$(docker logs "$feed" 2>&1)
[[ "$feed_log" != *'GET /repos/'* ]]
request=$(available)
master=$(docker exec "$panel" /command/s6-svstat -o pid /run/service/nginx-service)
before=$(main_hash)
old_workers=$(workers)
test -n "$old_workers"
change_module install "$(jq -c '.tag = "v0.0.0"' <<< "$request")" | expect '.state == "failed" and (.message | contains("重新确认"))'
test "$(main_hash)" = "$before"
test "$(workers)" = "$old_workers"
tag=$(jq -er '.tag' <<< "$request")
geoip2_release=$work/fixture/feed/repos/fixture/geoip2/releases/download/$tag
signature=$geoip2_release/module-release.sig.json
cp -- "$signature" "$work/good.json"
jq 'map_values("invalid")' "$signature" > "$work/invalid.json"
mv -- "$work/invalid.json" "$signature"
change_module install "$request" | expect '.state == "failed" and (.message | contains("签名不可信"))'
test "$(main_hash)" = "$before"
test "$(workers)" = "$old_workers"
mv -- "$work/good.json" "$signature"

asset=$(find "$geoip2_release" -maxdepth 1 -name 'geoip2-*.so')
test -f "$asset"
cp -- "$asset" "$work/good.so"
printf '\0' | dd of="$asset" bs=1 count=1 conv=notrunc status=none
change_module install "$request" | expect '.state == "failed" and (.message | contains("SHA-256"))'
test "$(main_hash)" = "$before"
test "$(workers)" = "$old_workers"
mv -- "$work/good.so" "$asset"
for action in install uninstall; do
    script=$(find "$geoip2_release" -maxdepth 1 -name "geoip2-$action-*.sh")
    cp -- "$script" "$work/good.sh"
    printf '!' | dd of="$script" bs=1 count=1 conv=notrunc status=none
    change_module install "$request" | expect '.state == "failed" and (.message | contains("SHA-256"))'
    test "$(main_hash)" = "$before"
    test "$(workers)" = "$old_workers"
    mv -- "$work/good.sh" "$script"
done
change_module install "$(available)" | expect '.state == "success" and (.script_detail | test("SHA-256 [0-9a-f]{64}.* 0$"))'
docker exec "$panel" apk info -e libmaxminddb >/dev/null
change_module install "$(available fixture-http v0.0.0)" | expect '.state == "success" and .module_id == "fixture-http" and (.script_detail | contains("版本 v0.0.0"))'
list | expect '.data.test_feed and ([.data.modules[] | select(.state == "enabled" and .loaded)] | length == 2)'
test "$(docker exec "$panel" /command/s6-svstat -o pid /run/service/nginx-service)" = "$master"
test "$(workers)" != "$old_workers"
test "$(main_hash)" != "$before"
docker exec "$panel" grep -F '/var/lib/nxpanel-nginx-modules-test/' /opt/nxpanel/nginx/nginx.conf >/dev/null
uninstall_request() { list | jq -ce --arg id "${1:-geoip2}" '.data.modules[] | select(.id == $id and .uninstall_confirmation) | {id,confirmation:.uninstall_confirmation}'; }
request=$(uninstall_request)
receipt=/var/lib/nxpanel-nginx-modules-test/receipts/geoip2.json
docker exec "$panel" mv "$receipt" "$receipt.saved"
list | expect '.data.feed_state == "online" and (.data.modules[] | select(.id == "geoip2") | .loaded and (.uninstall_confirmation | not))'
change_module uninstall "$request" | expect '.state == "failed" and (.message | contains("卸载凭据"))'
docker exec "$panel" mv "$receipt.saved" "$receipt"
index_signature=$work/fixture/feed/modules.sig.json
cp -- "$index_signature" "$work/good.json"
jq '.signatures |= map_values("invalid")' "$index_signature" > "$work/invalid.json"
mv -- "$work/invalid.json" "$index_signature"
list '?refresh=1' | expect '.data.feed_state == "stale" and ([.data.modules[] | select(.loaded)] | length == 2)'
docker exec "$panel" sh -ec 'printf "%s\n" "geoip2_proxy_recursive on;" > /opt/nxpanel/nginx/conf.d/module-test-dependency.conf
    "$NXPANEL_NGINX_BIN" -t -c "$NXPANEL_NGINX_CONF_PATH"'
change_module uninstall "$(uninstall_request)" | expect '.state == "failed" and (.message | contains("仍引用该模块"))'
docker exec "$panel" rm /opt/nxpanel/nginx/conf.d/module-test-dependency.conf
change_module uninstall "$(uninstall_request)" | expect '.state == "success" and .action == "uninstall"'
list | expect '(.data.modules[] | select(.id == "geoip2") | .loaded | not)'
docker exec "$panel" sh -ec 'test -f /var/lib/nxpanel-nginx-modules-test/*/geoip2.so'
test "$(docker exec "$panel" /command/s6-svstat -o pid /run/service/nginx-service)" = "$master"
docker stop "$feed" >/dev/null
change_module uninstall "$(uninstall_request fixture-http)" | expect '.state == "success" and .module_id == "fixture-http" and (.script_detail | test("SHA-256 [0-9a-f]{64}.* 0$"))'
mv -- "$work/good.json" "$index_signature"
docker start "$feed" >/dev/null
change_module install "$(available fixture-http)" | expect '.state == "success"'
docker rm -fv "$panel" >/dev/null
docker run -d --name "$replacement" --network "$network" \
    --mount "type=bind,source=$work/config,target=/opt/nxpanel/nginx" \
    -e "NXPANEL_AGENT_TOKEN=$(openssl rand -hex 32)" \
    -e NXPANEL_MODULE_TEST_FEED=http://module-feed/modules.json \
    -e "NXPANEL_MODULE_TEST_KEYS=$(cat "$work/fixture/trust.pub")" "$image" >/dev/null
started=false
for attempt in {1..25}; do
    if docker exec "$replacement" sh -ec 'test ! -d /var/lib/nxpanel-nginx-modules-test/active
        "$NXPANEL_NGINX_BIN" -t -c "$NXPANEL_NGINX_CONF_PATH"
        pid=$(/command/s6-svstat -o pid /run/service/nginx-service)
        test "$pid" -gt 1 && test -n "$(cat "/proc/$pid/task/$pid/children")"' >/dev/null 2>&1; then started=true; break; fi
    sleep 1
done
$started
printf 'Catalog and asset tampering, dependency install, two modules, receipt and dependency checks, offline uninstall, and container rebuild passed.\n'
