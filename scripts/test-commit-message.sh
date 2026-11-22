#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
validator="$script_dir/validate-commit-message.sh"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

tests_run=0

expect_valid() {
  local name=$1
  local message=$2
  local message_file="$tmp_dir/message"

  printf '%s\n' "$message" > "$message_file"
  if ! "$validator" "$message_file" >/dev/null 2>&1; then
    echo "Expected valid message ($name): $message" >&2
    exit 1
  fi
  tests_run=$((tests_run + 1))
}

expect_invalid() {
  local name=$1
  local message=$2
  local message_file="$tmp_dir/message"

  printf '%s\n' "$message" > "$message_file"
  if "$validator" "$message_file" >/dev/null 2>&1; then
    echo "Expected invalid message ($name): $message" >&2
    exit 1
  fi
  tests_run=$((tests_run + 1))
}

for type in feat fix docs style refactor perf test build ci chore revert; do
  expect_valid "allowed type: $type" "$type(core): 提交内容"
done

expect_valid "breaking change" "feat(api)!: 调整公开接口"
expect_valid "full-width colon" "fix(web)： 修复页面"
expect_valid "full-width colon with breaking change" "feat(api)!： 调整公开接口"
expect_valid "scope characters" "fix(api/v1-test_name.part): 修复接口"
expect_valid "body is unrestricted" $'docs(readme): 更新说明\n\n这里是正文。'
expect_valid "merge commit" "Merge branch 'dev' into main"
expect_valid "generated revert" 'Revert "feat(api): 调整接口"'
expect_valid "fixup commit" "fixup! feat(api): 调整接口"
expect_valid "squash commit" "squash! fix(web): 修复页面"

expect_invalid "missing scope" "fix: 修复页面"
expect_invalid "unknown type" "bug(web): 修复页面"
expect_invalid "uppercase type" "Fix(web): 修复页面"
expect_invalid "uppercase scope" "fix(Web): 修复页面"
expect_invalid "empty scope" "fix(): 修复页面"
expect_invalid "full-width colon without separator space" "fix(web)：修复页面"
expect_invalid "missing separator space" "fix(web):修复页面"
expect_invalid "multiple separator spaces" "fix(web):  修复页面"
expect_invalid "empty subject" "fix(web): "
expect_invalid "whitespace subject" "fix(web):    "

(
  git init -q --initial-branch=main "$tmp_dir/source"
  cd "$tmp_dir/source"
  export GIT_AUTHOR_NAME='Commit Message Test' GIT_AUTHOR_EMAIL='test@example.com'
  export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
  tree=$(git mktree </dev/null)
  parent=$(git -c commit.gpgSign=false commit-tree "$tree" -m 'Legacy commit')
  old_tip=$(git -c commit.gpgSign=false commit-tree "$tree" -p "$parent" -m 'Unformatted title')
  new_tip=$(git -c commit.gpgSign=false commit-tree "$tree" -p "$parent" -m 'fix(ci): Reword title')
  git update-ref refs/heads/main "$new_tip"

  git clone -q --no-local --single-branch . "$tmp_dir/checkout"
  cd "$tmp_dir/checkout"
  if git cat-file -e "$old_tip^{commit}" 2>/dev/null; then
    echo "Expected the pre-force-push commit to be missing from the checkout." >&2
    exit 1
  fi

  output=$("$script_dir/validate-commit-range.sh" "$old_tip" "$new_tip" origin)
  git cat-file -e "$old_tip^{commit}"
  if [[ "$output" != 'Validated 1 commit message(s).' ]]; then
    echo "Expected only the amended commit to be validated: $output" >&2
    exit 1
  fi
)
tests_run=$((tests_run + 1))

echo "Commit message validation tests passed ($tests_run cases)."
