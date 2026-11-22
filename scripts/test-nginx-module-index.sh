#!/usr/bin/env bash

set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

export REAL_GIT
REAL_GIT=$(command -v git)
export REMOTE="$temp_dir/origin.git" MOCK_PR="$temp_dir/pr.json" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_COMMITTER_NAME=test GIT_AUTHOR_EMAIL=test@example.com GIT_COMMITTER_EMAIL=test@example.com
export RUNNER_TEMP="$temp_dir/runner" GITHUB_OUTPUT="$temp_dir/output" GITHUB_STEP_SUMMARY="$temp_dir/summary"
export GITHUB_REPOSITORY=owner/nxpanel PR_NUMBER=123 GH_TOKEN=mock-reader
mkdir -p "$temp_dir/bin" "$RUNNER_TEMP"
cat > "$temp_dir/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -eu
case "$*" in
  'api repos/owner/nxpanel/pulls/123')
    [[ "$GH_TOKEN" == mock-reader || "$GH_TOKEN" == mock-signer ]]
    cat "$MOCK_PR"
    ;;
  'auth setup-git') [[ "$GH_TOKEN" == mock-reader || "$GH_TOKEN" == mock-signer ]] ;;
  *) exit 2 ;;
esac
EOF
cat > "$temp_dir/bin/git" <<'EOF'
#!/usr/bin/env bash
set -eu
if [[ "$1" == push && "$*" == *--force-with-lease=* ]]; then
  test "$GH_TOKEN" = mock-signer
  if [[ -n "${RACE_HEAD:-}" ]]; then
    "$REAL_GIT" --git-dir="$REMOTE" update-ref refs/heads/register "$RACE_HEAD"
  fi
fi
exec "$REAL_GIT" "$@"
EOF
chmod +x "$temp_dir/bin/gh" "$temp_dir/bin/git"

cd "$repo_dir"
go build -o "$temp_dir/catalog" ./cmd/nxpanel-module-catalog
go run ./cmd/nxpanel-update-package -keygen "$temp_dir/keys" -key-id test-index
export NXPANEL_MODULE_TRUST_KEYS NXPANEL_MODULE_SIGNING_KEYS NXPANEL_UPDATE_TRUST_KEYS=
NXPANEL_MODULE_TRUST_KEYS=$(cat "$temp_dir/keys/trust.pub")
NXPANEL_MODULE_SIGNING_KEYS=$(cat "$temp_dir/keys/signing.key")
export PATH="$temp_dir/bin:$PATH"

git init -q --bare -b main "$REMOTE"
git init -q -b main "$temp_dir/checkout"
cd "$temp_dir/checkout"
git remote add origin "$REMOTE"
path=catalog/nginx-modules
mkdir -p "$path"
printf '[]\n' > "$path/modules.json"
git add "$path/modules.json"
base=$(git commit-tree "$(git write-tree)" -m bootstrap)
git update-ref refs/heads/main "$base"
git push -q origin main
jq -n --arg key "module:$(printf '%032s' '' | base64 -w0)" \
  '[{id:"geoip2", name:"GeoIP2", description:"Test module", repository:"owner/geoip2", symbol:"ngx_http_geoip2_module", keys:$key}]' > "$path/modules.json"
git add "$path/modules.json"
head=$(git commit-tree "$(git write-tree)" -p "$base" -m register)
git update-ref refs/heads/register "$head"
git push -q origin register

pr() {
  jq -n --arg sha "$1" --arg repo "${2:-owner/nxpanel}" \
    '{state:"open", draft:false, base:{ref:"main"}, head:{sha:$sha, ref:"register", repo:{full_name:$repo}}}' > "$MOCK_PR"
}
index_workflow() {
  bash "$repo_dir/scripts/nginx-module-index.sh" "$@"
}
expect_failure() {
  local needle=$1 output
  shift
  if output=$("$@" 2>&1) || [[ "$output" != *"$needle"* ]]; then
    printf 'Expected failure matching %s, got:\n%s\n' "$needle" "$output" >&2
    exit 1
  fi
}

pr "$head" fork/nxpanel
expect_failure 'not a fork' index_workflow prepare
pr "$head"
index_workflow prepare
diff -u <(printf 'base_sha=%s\nhead_sha=%s\nhead_ref=register\n' "$base" "$head") "$GITHUB_OUTPUT"
export REVIEWED_BASE="$base" REVIEWED_HEAD="$head" REVIEWED_REF=register
index_workflow read-reviewed
work="$RUNNER_TEMP/nginx-module-index"
"$temp_dir/catalog" -sign-index -input "$work/proposed/modules.json" -output "$work/signed" -base-index-dir "$work/base"
unset NXPANEL_MODULE_SIGNING_KEYS
expect_failure 'Expected a regular index file' index_workflow read-check "$base" "$head"

changed=$(git commit-tree "$(git write-tree)" -p "$head" -m 'updated after review')
git push -q origin "$changed:refs/heads/register"
pr "$changed"
expect_failure 'changed after the review snapshot' index_workflow read-reviewed
git push -q --force origin "$head:refs/heads/register"
pr "$head"
GH_TOKEN= expect_failure 'requires its GitHub Actions token' index_workflow publish
GH_TOKEN=mock-signer RACE_HEAD="$changed" expect_failure 'Could not update the reviewed PR branch' index_workflow publish
test "$(git --git-dir="$REMOTE" rev-parse refs/heads/register)" = "$changed"
git push -q --force origin "$head:refs/heads/register"

GH_TOKEN=mock-signer index_workflow publish
test "$(git rev-parse HEAD)" = "$base"
signed=$(git --git-dir="$REMOTE" rev-parse refs/heads/register)
grep -F -- 'Approve workflows to run' "$GITHUB_STEP_SUMMARY" > /dev/null
test "$(git rev-parse "$signed^")" = "$head"
test "$(git diff-tree --no-commit-id --name-only -r "$head" "$signed")" = "$path/modules.sig.json"
diff -u <(printf '%s\n' refs/heads/main refs/heads/register) <(git --git-dir="$REMOTE" for-each-ref --format='%(refname)' refs/heads)
merged=$(git commit-tree "$(git rev-parse "$signed^{tree}")" -p "$base" -p "$signed" -m 'proposed merge')
index_workflow read-check "$base" "$merged"
"$temp_dir/catalog" -check-index -input "$work/proposed" -base-index-dir "$work/base"

printf 'unrelated\n' > extra.txt
git add extra.txt
mixed=$(git commit-tree "$(git write-tree)" -p "$head" -m 'unrelated files')
git push -q origin "$mixed:refs/heads/register" --force
pr "$mixed"
expect_failure 'must not change files outside' index_workflow prepare
expect_failure 'must not change files outside' index_workflow read-check "$base" "$mixed"

new_main=$(git commit-tree "$(git rev-parse "$base^{tree}")" -p "$base" -m 'advance main')
git push -q origin "$new_main:refs/heads/main"
pr "$mixed"
expect_failure 'main changed' index_workflow read-reviewed

printf 'Same-PR index signing checks passed.\n'
