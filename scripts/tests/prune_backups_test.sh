#!/usr/bin/env bash
# Тесты чистки старых снапшотов (prune_backups из lib.sh).
# Запуск: ./scripts/tests/prune_backups_test.sh
# Docker не нужен — функция работает только с файловой системой.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$TESTS_DIR/../lib.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

test_keeps_three_newest_deletes_older() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z
  snapshot proofreader-20260103T000000Z
  snapshot proofreader-20260104T000000Z
  snapshot proofreader-20260105T000000Z

  prune_backups "$TMPROOT" 3

  assert_entries \
    proofreader-20260103T000000Z \
    proofreader-20260104T000000Z \
    proofreader-20260105T000000Z
}

test_incomplete_snapshot_does_not_occupy_a_slot() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z
  snapshot proofreader-20260103T000000Z
  snapshot proofreader-20260104T000000Z --incomplete
  snapshot proofreader-20260105T000000Z

  prune_backups "$TMPROOT" 3

  # битый 04 снят сразу и слот не занял: лимит в 3 отсчитан по целым — 05, 03, 02
  assert_entries \
    proofreader-20260102T000000Z \
    proofreader-20260103T000000Z \
    proofreader-20260105T000000Z
}

test_incomplete_snapshot_deleted_even_under_the_limit() {
  snapshot proofreader-20260101T000000Z --incomplete
  snapshot proofreader-20260102T000000Z

  prune_backups "$TMPROOT" 3

  assert_entries proofreader-20260102T000000Z
}

test_zero_keep_disables_pruning_entirely() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z --incomplete
  snapshot proofreader-20260103T000000Z
  snapshot proofreader-20260104T000000Z

  prune_backups "$TMPROOT" 0

  assert_entries \
    proofreader-20260101T000000Z \
    proofreader-20260102T000000Z \
    proofreader-20260103T000000Z \
    proofreader-20260104T000000Z
}

test_leaves_non_snapshot_entries_alone() {
  # orphans/ — отложенные оригиналы PDF, их нет ни в одном снапшоте
  mkdir -p "$TMPROOT/orphans"
  printf 'pdf\n' > "$TMPROOT/orphans/works-42.pdf"
  printf 'note\n' > "$TMPROOT/README"
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z
  snapshot proofreader-20260103T000000Z

  prune_backups "$TMPROOT" 1

  assert_entries orphans README proofreader-20260103T000000Z
  [[ -f "$TMPROOT/orphans/works-42.pdf" ]] || fail "содержимое orphans/ снесено"
}

test_rejects_bad_keep_with_a_clear_message() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z

  local bad rc out
  for bad in abc -1 '' 2.5; do
    rc=0
    # в подскобках: die завершает процесс
    out="$( ( prune_backups "$TMPROOT" "$bad" ) 2>&1 )" || rc=$?
    (( rc != 0 )) || fail "лимит '$bad' принят молча (код $rc)"
    [[ "$out" == *prune_backups* ]] || fail "невнятная ошибка на лимите '$bad': $out"
  done

  assert_entries proofreader-20260101T000000Z proofreader-20260102T000000Z
}

test_reports_every_deleted_snapshot() {
  snapshot proofreader-20260101T000000Z
  snapshot proofreader-20260102T000000Z --incomplete
  snapshot proofreader-20260103T000000Z
  snapshot proofreader-20260104T000000Z

  local out
  out="$(prune_backups "$TMPROOT" 2 2>&1)"

  [[ "$out" == *proofreader-20260101T000000Z* ]] || fail "в отчёте нет удалённого 01: $out"
  [[ "$out" == *proofreader-20260102T000000Z* ]] || fail "в отчёте нет удалённого битого 02: $out"
  [[ "$out" != *proofreader-20260104T000000Z* ]] || fail "в отчёте упомянут уцелевший 04: $out"
}

run_tests
