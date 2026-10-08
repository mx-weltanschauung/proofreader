#!/usr/bin/env bash
# Тесты выбора снапшота для отправки (pick_snapshot из lib.sh).
# Запуск: ./scripts/tests/pick_snapshot_test.sh
# Docker и сеть не нужны — функция работает только с файловой системой.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$TESTS_DIR/../lib.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

assert_picked() {
  local expected="$1" got="$2"
  [[ "$got" == "$expected" ]] || fail "выбран не тот снапшот
    ожидалось: $expected
    получено:  $got"
}

test_latest_picks_the_newest_by_stamp() {
  snapshot proofreader-20260103T000000Z
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z

  assert_picked "$TMPROOT/proofreader-20260103T000000Z" \
    "$(pick_snapshot "$TMPROOT" latest)"
}

test_latest_skips_incomplete_snapshots() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z
  snapshot proofreader-20260103T000000Z --incomplete

  # битый снапшот новее всех, но заливать нечего: берётся последний целый
  assert_picked "$TMPROOT/proofreader-20260102T000000Z" \
    "$(pick_snapshot "$TMPROOT" latest)"
}

test_accepts_a_bare_snapshot_name() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z

  assert_picked "$TMPROOT/proofreader-20260101T000000Z" \
    "$(pick_snapshot "$TMPROOT" proofreader-20260101T000000Z)"
}

test_accepts_a_path_to_the_snapshot() {
  snapshot proofreader-20260101T000000Z

  assert_picked "$TMPROOT/proofreader-20260101T000000Z" \
    "$(pick_snapshot "$TMPROOT" "$TMPROOT/proofreader-20260101T000000Z")"
}

test_trailing_slash_does_not_break_the_path_form() {
  snapshot proofreader-20260101T000000Z

  # так путь приезжает из автодополнения оболочки
  assert_picked "$TMPROOT/proofreader-20260101T000000Z" \
    "$(pick_snapshot "$TMPROOT" "$TMPROOT/proofreader-20260101T000000Z/")"
}

test_refuses_an_explicitly_named_incomplete_snapshot() {
  snapshot proofreader-20260101T000000Z --incomplete

  local rc=0 out
  out="$( ( pick_snapshot "$TMPROOT" proofreader-20260101T000000Z ) 2>&1 )" || rc=$?
  (( rc != 0 )) || fail "битый снапшот принят молча"
  [[ "$out" == *INCOMPLETE* ]] || fail "в ошибке не сказано, что снапшот битый: $out"
}

test_refuses_a_missing_snapshot() {
  snapshot proofreader-20260101T000000Z

  local rc=0 out
  out="$( ( pick_snapshot "$TMPROOT" proofreader-20261231T000000Z ) 2>&1 )" || rc=$?
  (( rc != 0 )) || fail "несуществующий снапшот принят молча"
  [[ "$out" == *pick_snapshot* ]] || fail "невнятная ошибка: $out"
}

test_refuses_when_there_is_nothing_to_send() {
  snapshot proofreader-20260101T000000Z --incomplete
  mkdir -p "$TMPROOT/orphans"

  local rc=0 out
  out="$( ( pick_snapshot "$TMPROOT" latest ) 2>&1 )" || rc=$?
  (( rc != 0 )) || fail "пустой каталог бэкапов принят молча"
  [[ "$out" == *pick_snapshot* ]] || fail "невнятная ошибка: $out"
}

run_tests
