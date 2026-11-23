#!/usr/bin/env bash
set -euo pipefail

candidate=$(realpath -e "${1:?Expected candidate directory}")
recipes=${2:?Expected runtime recipe JSON}
arch_filter=${3:-}
smoke_dir=$(cd -- "$(dirname "$0")" && pwd -P)
go_bin=${GO:-go}
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

jq -e --arg local_images "${NXPANEL_MODULE_SMOKE_LOCAL_IMAGES:-0}" 'type == "array" and length > 0 and length <= 128 and all(.[];
    keys == ["arch","binary","image"] and (.arch | IN("amd64", "arm64")) and
    (.image | test("^(ghcr\\.io|docker\\.io)/([a-z0-9][a-z0-9_.-]*/)+[a-z0-9][a-z0-9_.-]*@sha256:[0-9a-f]{64}$") or
        ($local_images == "1" and test("^sha256:[0-9a-f]{64}$"))) and
    (.binary | test("^/([a-zA-Z0-9_-]+/)*[a-zA-Z0-9_-]+$")))' <<< "$recipes" >/dev/null
"$go_bin" build -o "$work/catalog" ./cmd/nxpanel-module-catalog
"$work/catalog" -check-candidate -input "$candidate"
manifest=$candidate/candidate.json
mmdb_commit=c454a21f31fd17f2843f16094e9afb3a14a42078
mmdb_sha=fc12d6470b35b70bd3e4534dd433d3e262a6c73f7682f85c344e832d4190f02b
if jq -e 'any(.modules[]; .id == "geoip2")' "$manifest" >/dev/null; then
    curl --proto '=https' -fsSL --max-time 30 --max-filesize 1048576 \
        "https://raw.githubusercontent.com/maxmind/MaxMind-DB/$mmdb_commit/test-data/GeoIP2-Country-Test.mmdb" -o "$work/test.mmdb"
    printf '%s  %s\n' "$mmdb_sha" "$work/test.mmdb" | sha256sum -c --status
else
    touch "$work/test.mmdb"
fi

declare -A verified
while IFS=$'\t' read -r image arch binary; do
    pull=missing
    if [[ "$image" == sha256:* ]]; then pull=never; fi
    runner=(docker run --rm --pull "$pull" --network none --platform "linux/$arch" --user 0:0 --pids-limit 128 --memory 512m
        --security-opt no-new-privileges --cap-drop ALL --cap-add CHOWN --cap-add SETUID --cap-add SETGID --cap-add KILL
        --mount "type=bind,source=$smoke_dir/check-release-action.sh,target=/verify.sh,readonly"
        --entrypoint /bin/sh)
    target=$("${runner[@]}" "$image" /verify.sh "$binary" target)
    IFS=$'\t' read -r os observed_arch runtime version digest <<< "$target"
    [[ "$observed_arch" == "$arch" ]]
    jq -n --arg os "$os" --arg arch "$arch" --arg runtime "$runtime" --arg version "$version" --arg digest "$digest" \
        '{os:$os,arch:$arch,runtime:$runtime,runtime_version:$version,binary_sha256:$digest}' > "$work/target.json"
    if ! match=$("$work/catalog" -match-target "$work/target.json" -input "$candidate"); then
        printf 'No unambiguous candidate for %s (%s)\n' "$target" "$image" >&2
        exit 1
    fi
    details=$(jq -er --argjson match "$match" '.modules[0] | [.id,.symbol,$match.name,.install.name,.uninstall.name] | @tsv' "$manifest")
    IFS=$'\t' read -r id symbol asset install uninstall <<< "$details"
    timeout 90 "${runner[@]}" \
        --mount "type=bind,source=$candidate,target=/candidate,readonly" \
        --mount "type=bind,source=$work/test.mmdb,target=/test.mmdb,readonly" \
        "$image" /verify.sh "$binary" "$id" "$symbol" "$asset" "$install" "$uninstall"
    verified[$asset]=1
done < <(jq -r --arg arch "$arch_filter" '.[] | select($arch == "" or .arch == $arch) | [.image,.arch,.binary] | @tsv' <<< "$recipes")
expected=$(jq -er --arg arch "$arch_filter" '[.assets[] | select($arch == "" or .target.arch == $arch).name] | unique | select(length > 0) | .[]' "$manifest")
while IFS= read -r asset; do
    [[ ${verified[$asset]+verified} ]] || { printf 'Candidate asset was not exercised: %s\n' "$asset" >&2; exit 1; }
done <<< "$expected"
