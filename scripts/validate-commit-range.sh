#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <base-sha> <head-sha>" >&2
  exit 2
fi

base_sha=$1
head_sha=$2
readonly zero_sha=0000000000000000000000000000000000000000
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
validator="$script_dir/validate-commit-message.sh"

if ! git cat-file -e "$head_sha^{commit}" 2>/dev/null; then
  echo "Head commit is unavailable: $head_sha" >&2
  exit 2
fi

if [[ "$base_sha" == "$zero_sha" ]]; then
  commits=("$head_sha")
else
  if ! git cat-file -e "$base_sha^{commit}" 2>/dev/null; then
    echo "Base commit is unavailable: $base_sha" >&2
    exit 2
  fi
  mapfile -t commits < <(git rev-list --reverse "$base_sha..$head_sha")
fi

if [[ ${#commits[@]} -eq 0 ]]; then
  echo "No new commit messages to validate."
  exit 0
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

failed=0
for commit in "${commits[@]}"; do
  message_file="$tmp_dir/$commit"
  git show -s --format=%B "$commit" > "$message_file"

  if ! "$validator" "$message_file"; then
    subject=$(git show -s --format=%s "$commit")
    echo "Commit $commit failed validation: $subject" >&2
    failed=1
  fi
done

if [[ $failed -ne 0 ]]; then
  exit 1
fi

echo "Validated ${#commits[@]} commit message(s)."
