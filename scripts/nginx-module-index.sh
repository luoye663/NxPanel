#!/usr/bin/env bash

set -euo pipefail

index_path=catalog/nginx-modules/modules.json
signature_path=catalog/nginx-modules/modules.sig.json
work_dir=$RUNNER_TEMP/nginx-module-index

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_index_only() {
  git diff --quiet --no-ext-diff --no-renames "$1" "$2" -- . \
    ":(top,exclude)$index_path" ":(top,exclude)$signature_path" ||
    fail 'An index PR must not change files outside modules.json and modules.sig.json.'
}

index_file() {
  local commit=$1 path=$2 listing mode kind oid size name
  listing=$(git ls-tree -l "$commit" -- "$path")
  read -r mode kind oid size name <<< "$listing"
  [[ "$mode" == 100644 && "$kind" == blob && -n "$oid" && "$name" == "$path" && "$size" =~ ^[0-9]+$ && "$size" -le 131072 ]] ||
    fail "Expected a regular index file of at most 128 KiB: $path"
  git cat-file blob "$commit:$path"
}

extract_index() {
  local commit=$1 dest=$2 required=$3
  mkdir -p "$dest"
  index_file "$commit" "$index_path" > "$dest/modules.json"
  if [[ "$required" == signature ]] || git cat-file -e "$commit:$signature_path" 2>/dev/null; then
    index_file "$commit" "$signature_path" > "$dest/modules.sig.json"
  fi
}

resolve_pr() {
  [[ "$PR_NUMBER" =~ ^[1-9][0-9]{0,9}$ ]] || fail 'Enter a positive integer PR number.'
  local metadata pr
  metadata=$(gh api "repos/$GITHUB_REPOSITORY/pulls/$PR_NUMBER")
  pr=$(jq -er --arg repo "$GITHUB_REPOSITORY" '
    select(.state == "open" and .draft == false and .base.ref == "main"
      and (.head.repo.full_name | ascii_downcase) == ($repo | ascii_downcase))
    | [.head.sha, .head.ref] | @tsv' <<< "$metadata") ||
    fail 'Use an open, non-draft PR from a branch in this repository to main (not a fork).'
  IFS=$'\t' read -r head_sha head_ref <<< "$pr"
  [[ "$head_sha" =~ ^[0-9a-f]{40}$ && "$head_ref" != main ]] || fail 'Invalid PR head.'
  git check-ref-format "refs/heads/$head_ref" || fail 'Invalid PR branch.'
  gh auth setup-git
  git fetch --no-tags origin \
    '+refs/heads/main:refs/nxpanel-index/base' \
    "+refs/heads/$head_ref:refs/nxpanel-index/head"
  base_sha=$(git rev-parse refs/nxpanel-index/base)
  [[ "$base_sha" == "$(git rev-parse HEAD)" ]] || fail 'main changed. Update the PR from main and run signing again.'
  [[ "$head_sha" == "$(git rev-parse refs/nxpanel-index/head)" ]] || fail 'The PR branch changed. Run signing again.'
  if [[ "${1:-}" == reviewed ]]; then
    [[ "$base_sha" == "$REVIEWED_BASE" && "$head_sha" == "$REVIEWED_HEAD" && "$head_ref" == "$REVIEWED_REF" ]] ||
      fail 'The PR or main changed after the review snapshot. Run signing again.'
  fi
  git merge-base --is-ancestor "$base_sha" "$head_sha" || fail 'Update the PR from the latest main before signing.'
  require_index_only "$base_sha" "$head_sha"
  if git diff --quiet "$base_sha" "$head_sha" -- "$index_path"; then
    fail 'The PR must change modules.json.'
  fi
  index_file "$head_sha" "$index_path" > /dev/null
}

case "${1:-}" in
  prepare)
    resolve_pr
    printf 'base_sha=%s\nhead_sha=%s\nhead_ref=%s\n' "$base_sha" "$head_sha" "$head_ref" >> "$GITHUB_OUTPUT"
    printf 'PR #%s: review index commit `%s` against `main` `%s` before approving.\n' \
      "$PR_NUMBER" "$head_sha" "$base_sha" >> "$GITHUB_STEP_SUMMARY"
    ;;
  read-reviewed)
    resolve_pr reviewed
    extract_index "$base_sha" "$work_dir/base" base
    mkdir -p "$work_dir/proposed"
    index_file "$head_sha" "$index_path" > "$work_dir/proposed/modules.json"
    ;;
  publish)
    [[ -n "${GH_TOKEN:-}" ]] || fail 'The signing job requires its GitHub Actions token.'
    resolve_pr reviewed
    export GIT_INDEX_FILE="$work_dir/publish.index"
    git read-tree "$head_sha"
    blob=$(git hash-object -w -- "$work_dir/signed/modules.sig.json")
    git update-index --add --cacheinfo "100644,$blob,$signature_path"
    tree=$(git write-tree)
    if [[ "$tree" == "$(git rev-parse "$head_sha^{tree}")" ]]; then
      printf 'The PR already contains this signature.\n'
      exit 0
    fi
    commit=$(git -c user.name=nxpanel-module-publisher \
      -c user.email=nxpanel-module-publisher@users.noreply.github.com \
      commit-tree "$tree" -p "$head_sha" -m 'chore(nginx-modules): sign module index')
    ref="refs/heads/$head_ref"
    git push --force-with-lease="$ref:$head_sha" origin "$commit:$ref" ||
      fail 'Could not update the reviewed PR branch. If the branch changed, run signing again.'
    printf 'PR #%s was updated. In its merge box, choose **Approve workflows to run** to run checks for `%s`.\n' \
      "$PR_NUMBER" "$commit" >> "$GITHUB_STEP_SUMMARY"
    ;;
  read-check)
    require_index_only "$2" "$3"
    extract_index "$2" "$work_dir/base" base
    extract_index "$3" "$work_dir/proposed" signature
    ;;
  *)
    fail 'Expected prepare, read-reviewed, publish, or read-check.'
    ;;
esac
