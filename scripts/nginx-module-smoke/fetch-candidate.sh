#!/usr/bin/env bash
set -euo pipefail

repo=${1:?Expected owner/repo}
tag=${2:?Expected release tag}
digest=${3:?Expected candidate.json SHA-256}
out=${4:?Expected a new output directory}
[[ "$repo" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$ ]]
[[ "$tag" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$ && "$digest" =~ ^[a-f0-9]{64}$ ]]
mkdir "$out"
fetch() {
    curl --proto '=https' --proto-redir '=https' -fsSL --max-redirs 3 --max-time 60 --max-filesize "$2" \
        "https://github.com/$repo/releases/download/$tag/$1" -o "$out/$1"
}
fetch candidate.json 131072
printf '%s  %s\n' "$digest" "$out/candidate.json" | sha256sum -c --status
jq -er --arg repo "$repo" --arg tag "$tag" '
    def id: type == "string" and test("^[a-z][a-z0-9_-]{0,39}$");
    .modules as $modules | .assets as $assets |
    select(($modules | type == "array" and length > 0 and length <= 32) and
           ($assets | type == "array" and length > 0 and length <= 128)) |
    select(all($modules[]; .kind == "nginx_dynamic_so" and (.id | id) and .release == {repository: $repo, tag: $tag})) |
    select(all($modules[]; .id as $id | any($assets[]; .module == $id))) |
    [$modules[] | .id as $id | (.install + {prefix: ($id + "-install-"), extension: ".sh", max: 131072}),
                                 (.uninstall + {prefix: ($id + "-uninstall-"), extension: ".sh", max: 131072})] +
    [$assets[] | select(.module | id) | . + {prefix: (.module + "-"), extension: ".so", max: 4194304}] |
    select(length == (2 * ($modules | length) + ($assets | length))) |
    select(all(.[]; (.sha256 | type == "string" and test("^[a-f0-9]{64}$")) and
                    (.size | type == "number" and floor == . and . > 0) and .size <= .max and
                    .name == (.prefix + .sha256 + .extension))) |
    unique_by(.name)[] | [.name, .size, .sha256] | @tsv' "$out/candidate.json" > "$out/assets.tsv"
while IFS=$'\t' read -r name size sha; do
    [[ "$name" =~ ^[a-z][a-z0-9_-]{0,50}-[a-f0-9]{64}\.(so|sh)$ && "$sha" =~ ^[a-f0-9]{64}$ ]]
    fetch "$name" "$size"
    [[ $(wc -c < "$out/$name") -eq "$size" ]]
    printf '%s  %s\n' "$sha" "$out/$name" | sha256sum -c --status
done < "$out/assets.tsv"
