#!/usr/bin/env bash
set -euo pipefail

builds=${1:?Specify the test build directory}
example=${NXPANEL_MODULE_EXAMPLE:-$PWD/local-module-repos/nginx-module-assets-example}
source_dir=$example/modules/geoip2
if [[ ! -f "$source_dir/package-scripts.sh" ]]; then
    printf 'Missing example asset repository: %s. Set NXPANEL_MODULE_EXAMPLE to its checkout.\n' "$example" >&2
    exit 1
fi
sh "$source_dir/package-scripts.sh" "$builds/geoip2"
mkdir -p "$builds/fixture-http"
for action in install uninstall; do
    { cat "$source_dir/lifecycle.sh"; printf '\n%s_module "$@"\n' "$action"; } > "$builds/fixture-http/$action.sh"
    sh -n "$builds/fixture-http/$action.sh"
done
