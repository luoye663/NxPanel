#!/usr/bin/env bash
set -euo pipefail

image=${1:-nxpanel:update-dev}
feed_image=${2:-nginx:alpine}
fixture=$(realpath tmp/update-fixture)
[[ -f "$fixture/feed/index.json" && -f "$fixture/trust.pub" ]] || { echo 'Run make update-fixture first' >&2; exit 1; }
nonce=$(openssl rand -hex 6)
network=nxpanel-update-test-$nonce
panel=$network-panel
feed=$network-feed
cookie=$(mktemp)

cleanup() {
    local status=$?
    if [ "$status" -ne 0 ]; then
        docker logs --tail 60 "$panel" >&2 || true
    fi
    docker rm -f "$panel" "$feed" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
    rm -f "$cookie"
}
trap cleanup EXIT

docker network create "$network" >/dev/null
docker run -d --name "$feed" --network "$network" --network-alias update-feed \
    --mount "type=bind,source=$fixture/feed,target=/usr/share/nginx/html,readonly" "$feed_image" >/dev/null
docker run -d --name "$panel" --network "$network" -p 127.0.0.1::18888 \
    -e "NXPANEL_API_LOGIN_PATH=/upgrade-$nonce" \
    -e "NXPANEL_AGENT_TOKEN=$(openssl rand -hex 32)" \
    -e NXPANEL_UPDATE_TEST_FEED=http://update-feed/index.json \
    -e "NXPANEL_UPDATE_TEST_KEYS=$(cat "$fixture/trust.pub")" \
    -e NXPANEL_UPDATE_TEST_FAIL_VERSION=v0.0.3-rc.1 \
    "$image" >/dev/null

base=https://$(docker port "$panel" 18888/tcp)
api=$base/api/v1/upgrade-$nonce
password="Nx!$(openssl rand -hex 20)"
credentials=$(jq -cn --arg password "$password" '{username:"admin",password:$password}')

wait_for() {
    local predicate=$1
    local result
    for attempt in {1..150}; do
        if result=$(curl -ksSf --max-time 2 -b "$cookie" "$api/system/upgrade" 2>/dev/null) && jq -e "$predicate" <<< "$result" >/dev/null; then
            jq -c .data <<< "$result"
            return
        fi
        sleep 1
    done
    printf 'Timed out waiting for %s\n' "$predicate" >&2
    return 1
}

for attempt in {1..60}; do
    if curl -ksSf --max-time 2 "$base/upgrade-$nonce" >/dev/null 2>&1; then break; fi
    sleep 1
done
curl -ksSf --max-time 5 -H 'Content-Type: application/json' --data "$credentials" "$api/setup/admin" | jq -e .success >/dev/null
curl -ksSf --max-time 5 -c "$cookie" -H 'Content-Type: application/json' --data "$credentials" "$api/auth/login" | jq -e .success >/dev/null
csrf=$(awk '$6 == "csrf-token" { print $7 }' "$cookie")
test -n "$csrf"

mutate() {
    curl -ksSf --max-time 25 -b "$cookie" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' -X "$1" --data "$3" "$api/system/upgrade/$2"
}

mutate POST check '{}' | jq -e '.data.installable and .data.latest_version == "v0.0.2"' >/dev/null
mutate POST prepare '{"version":"v0.0.2"}' | jq -e .success >/dev/null
prepared_log='.data.task.phase == "prepared" and .data.current_version == "v0.0.1" and .data.download == null and .data.progress.task_id == .data.task.id and [.data.progress.events[].stage] == ["downloading", "validating", "prepared"]'
task=$(wait_for "$prepared_log" | jq -c '{id:.task.id}')
mutate POST cancel "$task" | jq -e '.data.task == null and .data.progress.stage == "cancelled"' >/dev/null
mutate POST prepare '{"version":"v0.0.2"}' | jq -e .success >/dev/null
wait_for "$prepared_log" >/dev/null
docker restart "$panel" >/dev/null
base=https://$(docker port "$panel" 18888/tcp)
api=$base/api/v1/upgrade-$nonce
task=$(wait_for "$prepared_log" | jq -c '{id:.task.id}')
nginx_pid=$(docker exec "$panel" /command/s6-svstat -o pid /run/service/nginx-service)
mutate POST activate "$task" >/dev/null || true
wait_for '.data.task.phase == "committed" and .data.current_version == "v0.0.2" and .data.progress.stage == "committed" and [.data.progress.events[].stage] == ["downloading", "validating", "prepared", "applying", "trial", "committed"]' >/dev/null
curl -ksSf --max-time 5 -b "$cookie" "$api/system/overview" | jq -e '.data.agent.available and .data.agent.version == "v0.0.2"' >/dev/null
printf 'Stable update committed; a restart before activation kept the original version.\n'

mutate PUT channel '{"channel":"preview"}' | jq -e '.data.installable and .data.latest_version == "v0.0.3-rc.1"' >/dev/null
mutate POST prepare '{"version":"v0.0.3-rc.1"}' | jq -e .success >/dev/null
task=$(wait_for '.data.task.phase == "prepared" and .data.task.version == "v0.0.3-rc.1"' | jq -c '{id:.task.id}')
mutate POST activate "$task" >/dev/null || true
wait_for '.data.task.phase == "rolled_back" and .data.current_version == "v0.0.2" and .data.channel == "preview" and .data.progress.stage == "rolled_back" and ([.data.progress.events[].stage] | map(select(. != "restoring"))) == ["downloading", "validating", "prepared", "applying", "trial", "verifying_previous", "rolled_back"]' >/dev/null
curl -ksSf --max-time 5 -b "$cookie" "$api/system/overview" | jq -e '.data.agent.available and .data.agent.version == "v0.0.2"' >/dev/null
test "$(docker exec "$panel" /command/s6-svstat -o pid /run/service/nginx-service)" = "$nginx_pid"
printf 'Failed preview rolled back with progress history, session, channel, agent and Nginx process intact.\n'
