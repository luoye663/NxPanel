#!/usr/bin/env bash

set -euo pipefail

readonly allowed_types="feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert"
readonly conventional_pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)\([a-z0-9][a-z0-9._/-]*\)!?(:|：) [^[:space:]].*$'

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <commit-message-file>" >&2
  exit 2
fi

message_file=$1
if [[ ! -r "$message_file" ]]; then
  echo "Cannot read commit message file: $message_file" >&2
  exit 2
fi

IFS= read -r header < "$message_file" || true

case "$header" in
  "Merge "*|"Revert "*|"fixup! "*|"squash! "*)
    exit 0
    ;;
esac

if [[ "$header" =~ $conventional_pattern ]]; then
  exit 0
fi

cat >&2 <<EOF
Invalid commit message:
  $header

Expected:
  type(scope): subject
  type(scope)： subject
  type(scope)!: breaking change subject

Example:
  fix(web): 修复失败超时输入框描述

Allowed types: $allowed_types
The scope is required and must use lowercase letters, digits, '.', '_', '/', or '-'.
Use an ASCII or full-width colon followed by exactly one space, and provide a non-empty subject.
EOF
exit 1
