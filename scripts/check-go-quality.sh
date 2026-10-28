#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd "$script_dir/.." && pwd)
staticcheck_baseline_file="$repo_dir/.lint-baseline/staticcheck.txt"
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

cd "$repo_dir"

# web 包用 //go:embed all:dist 嵌入前端产物，embed 要求该目录存在且有文件；
# 未构建过前端时整个门禁无法编译 web 包，这里给出明确指引（CI 中由工作流预先放占位文件）。
if [[ -z "$(ls -A web/dist 2>/dev/null)" ]]; then
  echo "web/dist 不存在或为空：go vet/staticcheck/deadcode 需要编译 web 包（//go:embed all:dist）。" >&2
  echo "请先构建前端：make web（或 cd web && pnpm install --frozen-lockfile && pnpm build）。" >&2
  exit 1
fi

echo "Checking gofmt..."
find . -type f -name '*.go' \
  -not -path './web/node_modules/*' \
  -not -path './.git/*' \
  -exec gofmt -l {} + \
  | sed 's|^\./||' \
  | LC_ALL=C sort > "$work_dir/gofmt.txt"
if [[ -s "$work_dir/gofmt.txt" ]]; then
  echo "The following Go files are not formatted:" >&2
  cat "$work_dir/gofmt.txt" >&2
  exit 1
fi

echo "Checking interface spelling..."
find . -type f -name '*.go' \
  -not -path './web/node_modules/*' \
  -not -name '*_test.go' \
  -exec grep -nE 'interface[[:space:]]*\{[[:space:]]*\}' {} + \
  > "$work_dir/interface-legacy.txt" || true
if [[ -s "$work_dir/interface-legacy.txt" ]]; then
  echo "Use any instead of interface{} in production code:" >&2
  cat "$work_dir/interface-legacy.txt" >&2
  exit 1
fi

echo "Running go vet..."
go vet ./...

echo "Running staticcheck..."
set +e
go tool staticcheck ./... \
  > "$work_dir/staticcheck.output" \
  2> "$work_dir/staticcheck.error"
staticcheck_status=$?
set -e

sed -E \
	-e 's|^\./||' \
	-e 's|^([^:]+):[0-9]+:[0-9]+: |\1: |' \
	"$work_dir/staticcheck.output" \
	| LC_ALL=C sort > "$work_dir/staticcheck.current"
LC_ALL=C sort "$staticcheck_baseline_file" > "$work_dir/staticcheck.baseline"

if ! cmp -s "$staticcheck_baseline_file" "$work_dir/staticcheck.baseline"; then
  echo "staticcheck baseline must remain sorted: $staticcheck_baseline_file" >&2
  exit 1
fi

comm -13 "$work_dir/staticcheck.baseline" "$work_dir/staticcheck.current" > "$work_dir/staticcheck.new"
comm -23 "$work_dir/staticcheck.baseline" "$work_dir/staticcheck.current" > "$work_dir/staticcheck.resolved"

if [[ -s "$work_dir/staticcheck.new" || -s "$work_dir/staticcheck.resolved" ]]; then
  if [[ -s "$work_dir/staticcheck.new" ]]; then
    echo "New staticcheck findings:" >&2
    cat "$work_dir/staticcheck.new" >&2
  fi
  if [[ -s "$work_dir/staticcheck.resolved" ]]; then
    echo "Resolved staticcheck findings must be removed from $staticcheck_baseline_file:" >&2
    cat "$work_dir/staticcheck.resolved" >&2
  fi
  if [[ -s "$work_dir/staticcheck.output" ]]; then
    echo "Full staticcheck output:" >&2
    cat "$work_dir/staticcheck.output" >&2
  fi
  exit 1
fi

if [[ -s "$work_dir/staticcheck.error" ]]; then
  # Go may print module/toolchain download progress to stderr on a clean runner.
  # Keep that bootstrap noise out of the finding baseline, but never hide a
  # substantive staticcheck or tool execution error.
  sed '/^go: downloading /d' "$work_dir/staticcheck.error" > "$work_dir/staticcheck.error.filtered"
  if [[ -s "$work_dir/staticcheck.error.filtered" ]]; then
    echo "staticcheck failed:" >&2
    cat "$work_dir/staticcheck.error.filtered" >&2
    exit 1
  fi
fi

if [[ $staticcheck_status -gt 1 ]]; then
  echo "staticcheck exited with status $staticcheck_status" >&2
  cat "$work_dir/staticcheck.output" >&2
  exit "$staticcheck_status"
fi

if [[ $staticcheck_status -eq 1 && ! -s "$work_dir/staticcheck.output" ]]; then
  echo "staticcheck exited with status 1 without diagnostics" >&2
  exit 1
fi

# deadcode 带 -test：仅被测试引用的辅助方法（如 Freeze/ClientCount）不算死代码，
# 只拦截从 main 与测试都不可达的真死代码。
# 该步需要整程序加载语法与类型信息，冷缓存实测峰值约 1.7GB；下面用 GOGC/GOMEMLIMIT 压到约 1.2GB。
# 内存受限的 CI runner 若仍被 OOM 杀掉（日志为 "signal: killed"），可设 RW_SKIP_DEADCODE=1 跳过本步
# （go vet 与 staticcheck 的 U1000 仍会执行）；给 runner 加内存到 2GB+ 后可去掉该开关。
if [[ "${RW_SKIP_DEADCODE:-0}" == "1" ]]; then
  echo "deadcode 已跳过（RW_SKIP_DEADCODE=1）"
else
  echo "Running deadcode..."
  set +e
  GOGC=50 GOMEMLIMIT=800MiB go tool deadcode -test ./... \
    > "$work_dir/deadcode.output" \
    2> "$work_dir/deadcode.error"
  deadcode_status=$?
  set -e

  if [[ -s "$work_dir/deadcode.error" ]]; then
    sed '/^go: downloading /d' "$work_dir/deadcode.error" > "$work_dir/deadcode.error.filtered"
    if [[ -s "$work_dir/deadcode.error.filtered" ]]; then
      echo "deadcode failed:" >&2
      cat "$work_dir/deadcode.error.filtered" >&2
      exit 1
    fi
  fi

  if [[ $deadcode_status -ne 0 || -s "$work_dir/deadcode.output" ]]; then
    echo "Dead code detected or deadcode failed:" >&2
    cat "$work_dir/deadcode.output" >&2
    exit 1
  fi
fi

echo "Go quality checks passed."