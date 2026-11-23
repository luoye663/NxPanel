#!/usr/bin/env bash
set -euo pipefail

output=${1:?provide a new directory for the public NxPanel trust policy}
base=https://raw.githubusercontent.com/luoye663/NxPanel/main/catalog/trust
mkdir -- "$output"
curl -fsS --max-time 30 --max-filesize 131072 "$base/trust.json" -o "$output/trust.json"
curl -fsS --max-time 30 --max-filesize 512 "$base/trust.sig" -o "$output/trust.sig"
