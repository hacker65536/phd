#!/usr/bin/env bash
#
# phd スモークテスト。実 AWS（読み取りのみ）に対して主要シナリオを通し、
# 終了コードと出力内容で PASS/FAIL を判定する。
#
# Usage: scripts/smoke.sh [profile]
#   profile 既定: my-sso-profile（引数または AWS_PROFILE で上書き）
#   別の config を使う場合は AWS_CONFIG_FILE を export してから実行する。

set -uo pipefail

export AWS_CONFIG_FILE="${AWS_CONFIG_FILE:-$HOME/.aws/config}"
PROFILE="${1:-${AWS_PROFILE:-my-sso-profile}}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN="/tmp/phd"

echo "=== build ==="
( cd "$REPO_DIR" && go build -o "$BIN" . ) || { echo "build failed"; exit 1; }

PASS=0
FAIL=0

#######################################
# 1シナリオを実行して判定する。
# Arguments: name expect_exit grep_pattern -- cmd...
#######################################
run() {
    local name="$1" expect_exit="$2" pat="$3"; shift 3
    local rc ok=1
    "$@" >/tmp/smoke_out 2>&1; rc=$?
    [[ "$rc" == "$expect_exit" ]] || ok=0
    if [[ -n "$pat" ]]; then
        grep -qE "$pat" /tmp/smoke_out || ok=0
    fi
    if [[ $ok == 1 ]]; then
        echo "  PASS  $name"; PASS=$((PASS+1))
    else
        echo "  FAIL  $name (rc=$rc want=$expect_exit pat=$pat)"; FAIL=$((FAIL+1))
    fi
}

B=("$BIN" events --profile "$PROFILE")

# キャッシュを温める
"${B[@]}" >/dev/null 2>&1 || true

echo "== 既定 / 期間 / 絞り込み =="
run "default (open+upcoming)"        0 "SERVICE|No events"  "${B[@]}"
run "status upcoming + within 120d"  0 "STATUS|No events"   "${B[@]}" --status upcoming --within 120d
run "service + event-type regex"     0 "."                  "${B[@]}" --service EC2 --event-type RETIREMENT
run "since 30d (history)"            0 "."                  "${B[@]}" --since 30d
run "starting range (audit)"         0 "."                  "${B[@]}" --starting 2026-06-01..2026-12-01
run "filter-region"                  0 "."                  "${B[@]}" --filter-region ap-northeast-1
run "no-merge"                       0 "."                  "${B[@]}" --no-merge --service ECS

echo "== ロールアップ =="
run "group-by type"                  0 "EVENT_TYPE"         "${B[@]}" --group-by type
run "group-by topic (TOPIC col)"     0 "TOPIC"              "${B[@]}" --service LAMBDA --group-by topic --open-since 0
run "topic + show-occurrences"       0 "occurrence"         "${B[@]}" --service LAMBDA --group-by topic --open-since 0 --show-occurrences

echo "== 詳細 / リソース =="
run "show-details (description)"     0 "Kubernetes|サポート|EKS" "${B[@]}" --service EKS --status upcoming --show-details
run "show-resources (>10 accounts)"  0 "cluster|resource"   "${B[@]}" --service EKS --status upcoming --show-resources

echo "== 出力フォーマット =="
if "${B[@]}" --service EKS --status upcoming --format json 2>/dev/null | python3 -c "import json,sys;json.load(sys.stdin)" 2>/dev/null; then
    echo "  PASS  json valid"; PASS=$((PASS+1))
else
    echo "  FAIL  json valid"; FAIL=$((FAIL+1))
fi
run "csv header"                     0 "EventTypeCode|Service" "${B[@]}" --service EKS --status upcoming --format csv
run "markdown table"                 0 '^\| SERVICE'          "${B[@]}" --service EKS --status upcoming --format markdown

echo "== スコープ / 異常系 =="
run "scope account (self)"           0 "."                  "${B[@]}" --scope account --status open,upcoming
run "invalid --group-by -> error"    1 "invalid --group-by" "${B[@]}" --group-by bogus
run "invalid --scope -> error"       1 "invalid --scope"    "${B[@]}" --scope galaxy

echo
echo "RESULT: PASS=$PASS FAIL=$FAIL"
[[ $FAIL == 0 ]]
