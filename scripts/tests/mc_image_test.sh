#!/usr/bin/env bash
# Тесты образа клиента MinIO: ensure_mc_image и mc_run из lib.sh.
# Запуск: ./scripts/tests/mc_image_test.sh — docker подменён функцией.
#
# minio/mc больше не публикуется (Docker Hub: pull access denied, dl.min.io:
# 410 Gone), поэтому образ собирается из docker/mc/Dockerfile. Сторожится
# три вещи: тег не разошёлся с релизом в Dockerfile, сборка не загрязняет
# stdout mc_run (его читают через $(…)), и ни один скрипт не зовёт прежний
# образ.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$TESTS_DIR/../lib.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

NET=proofreader_default S3_KEY=k S3_SECRET=s

# fake_docker <есть ли образ: yes|no> — docker, пишущий вызовы в $TMPROOT/calls.
# Сборка и запуск шумят в stdout нарочно: так видно, чей вывод куда уехал.
fake_docker() {
  HAVE_IMAGE="$1"
  docker() {
    printf '%s\n' "$*" >> "$TMPROOT/calls"
    case "$1 ${2:-}" in
      "image inspect") [[ "$HAVE_IMAGE" == yes ]] ;;
      "build "*) cat > /dev/null; echo "Step 1/9 : FROM golang"; HAVE_IMAGE=yes ;;
      "run "*) echo "MC-OUT" ;;
      *) return 1 ;;
    esac
  }
}

test_image_tag_matches_the_release_in_the_dockerfile() {
  local release
  release="$(sed -nE 's/^ARG MC_RELEASE=(.*)$/\1/p' "$REPO_ROOT/docker/mc/Dockerfile")"
  [[ -n "$release" ]] || fail "в docker/mc/Dockerfile нет ARG MC_RELEASE"
  [[ "$MC_IMAGE" == "proofreader-mc:$release" ]] ||
    fail "тег MC_IMAGE ($MC_IMAGE) разошёлся с релизом в Dockerfile ($release): машина с прежним образом в кэше осталась бы на старом mc"
}

test_missing_image_is_built_and_stdout_stays_mc_output() {
  local out
  fake_docker no
  out="$(mc_run ls seaweed/x 2>/dev/null)"
  [[ "$out" == "MC-OUT" ]] || fail "stdout mc_run обязан быть выводом mc, а не сборки: '$out'"
  grep -q "^build -t $MC_IMAGE -$" "$TMPROOT/calls" ||
    fail "образа нет — должен собраться под тегом $MC_IMAGE:
$(cat "$TMPROOT/calls")"
}

test_present_image_is_not_rebuilt() {
  fake_docker yes
  mc_run ls seaweed/x > /dev/null 2>&1
  if grep -q '^build' "$TMPROOT/calls"; then
    fail "образ уже есть — пересобирать нельзя:
$(cat "$TMPROOT/calls")"
  fi
  return 0
}

test_mc_run_runs_the_own_image() {
  local run_line
  fake_docker yes
  mc_run ls seaweed/x > /dev/null 2>&1
  run_line="$(grep '^run ' "$TMPROOT/calls")"
  [[ "$run_line" == *" $MC_IMAGE ls seaweed/x" ]] ||
    fail "mc_run должен запускать $MC_IMAGE с аргументами mc: $run_line"
}

test_failed_build_stops_mc_run() {
  local rc=0
  docker() {
    case "$1 ${2:-}" in
      "image inspect") return 1 ;;
      "build "*) cat > /dev/null; return 1 ;;
      "run "*) echo "ran" >> "$TMPROOT/ran" ;;
    esac
  }
  ( mc_run ls seaweed/x ) > /dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "несобравшийся образ должен ронять mc_run"
  [[ ! -e "$TMPROOT/ran" ]] || fail "после неудачной сборки mc запускаться не должен"
}

# Прежний образ не тянется ниоткуда; скрипт, всё ещё зовущий его, упадёт
# только на машине без кэша — то есть ровно на свежем сервере.
test_no_script_runs_minio_mc_anymore() {
  local hits
  hits="$(grep -nE 'minio/mc' "$REPO_ROOT"/scripts/*.sh | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' || true)"
  [[ -z "$hits" ]] || fail "скрипты всё ещё зовут minio/mc:
$hits"
}

run_tests
