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
expect_invalid "full-width colon" "fix(web)：修复页面"
expect_invalid "missing separator space" "fix(web):修复页面"
expect_invalid "multiple separator spaces" "fix(web):  修复页面"
expect_invalid "empty subject" "fix(web): "
expect_invalid "whitespace subject" "fix(web):    "

echo "Commit message validation tests passed ($tests_run cases)."
