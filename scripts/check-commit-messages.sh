#!/usr/bin/env bash

set -euo pipefail

base="${1:-}"
head="${2:-HEAD}"

if [[ -z "$base" ]]; then
  echo "usage: check-commit-messages.sh <base-sha> [head-sha]" >&2
  exit 1
fi

# 类型前缀必须使用英文小写，作用域可选（如 feat(notify):），主题文本允许中文等任意 UTF-8 字符
pattern='^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([a-zA-Z0-9._/-]+\))?: .+$'

invalid=0
while IFS= read -r line; do
  sha="${line%% *}"
  subject="${line#* }"

  if [[ "$subject" == Merge\ * ]]; then
    continue
  fi

  if [[ ! "$subject" =~ $pattern ]]; then
    echo "::error title=Invalid commit message::${sha} ${subject}"
    invalid=1
  fi
done < <(git log --format='%H %s' "$base..$head")

if [[ "$invalid" -ne 0 ]]; then
  echo "Commit message subjects must use: type: subject 或 type(scope): subject"
  echo "Allowed types: build, chore, ci, docs, feat, fix, perf, refactor, revert, style, test"
  echo "Subject text may be any UTF-8 text (Chinese is fine), but the type prefix must be English."
  exit 1
fi

echo "Commit messages OK."