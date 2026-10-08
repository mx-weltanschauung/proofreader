#!/usr/bin/env bash
# Тесты разбора раскладки снапшота (snapshot_meta из lib.sh).
# Запуск: ./scripts/tests/snapshot_meta_test.sh
# Docker не нужен — функция работает только с файловой системой.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$TESTS_DIR/../lib.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

# new_snapshot <name> — снапшот новой раскладки: db.dump и repo.bundle внутри
# meta.tar, манифест рядом с ним (его читают, не распаковывая архив).
new_snapshot() {
  local name="$1" dir="$TMPROOT/$1" staging
  mkdir -p "$dir/files"
  printf '{"timestamp":"%s"}\n' "$name" > "$dir/manifest.json"
  staging="$(mktemp -d)"
  printf 'dump\n' > "$staging/db.dump"
  printf 'bundle\n' > "$staging/repo.bundle"
  tar -C "$staging" -cf "$dir/meta.tar" db.dump repo.bundle
  rm -rf "$staging"
}

# workdir — пустой каталог под распаковку, как его заводит вызывающий
workdir() { mktemp -d "$TMPROOT/work.XXXXXX"; }

test_old_layout_returns_the_snapshot_dir_itself() {
  snapshot proofreader-20260101T000000Z
  local out
  out="$(snapshot_meta "$TMPROOT/proofreader-20260101T000000Z" "$(workdir)")"

  [[ "$out" == "$TMPROOT/proofreader-20260101T000000Z" ]] ||
    fail "старая раскладка должна отдавать сам снапшот, получено: $out"
  [[ "$(cat "$out/db.dump")" == dump ]] || fail "db.dump недоступен по отданному пути"
}

test_new_layout_unpacks_the_archive_into_the_workdir() {
  new_snapshot proofreader-20260101T000000Z
  local snap work out
  snap="$TMPROOT/proofreader-20260101T000000Z"
  work="$(workdir)"
  out="$(snapshot_meta "$snap" "$work")"

  [[ "$out" == "$work" ]] || fail "новая раскладка должна отдавать каталог распаковки, получено: $out"
  [[ "$(cat "$out/db.dump")" == dump ]] || fail "db.dump не распакован"
  [[ "$(cat "$out/repo.bundle")" == bundle ]] || fail "repo.bundle не распакован"
}

test_new_layout_leaves_the_snapshot_untouched() {
  new_snapshot proofreader-20260101T000000Z
  local snap
  snap="$TMPROOT/proofreader-20260101T000000Z"
  snapshot_meta "$snap" "$(workdir)" > /dev/null

  # снапшот только читают: распаковка не должна оседать в нём самом
  local got expected
  got="$(entries "$snap")"
  expected="$(printf '%s\n' files manifest.json meta.tar | sort)"
  [[ "$got" == "$expected" ]] || fail "снапшот изменён распаковкой:
$got"
}

test_manifest_stays_readable_without_unpacking() {
  new_snapshot proofreader-20260101T000000Z
  # verify-restore.sh и restore.sh читают манифест грепом прямо из снапшота
  grep -q '"timestamp"' "$TMPROOT/proofreader-20260101T000000Z/manifest.json" ||
    fail "манифест не читается из снапшота без распаковки"
}

test_snapshot_without_dump_or_archive_is_rejected() {
  local snap rc out
  snap="$TMPROOT/proofreader-20260101T000000Z"
  mkdir -p "$snap/files"
  rc=0
  out="$( ( snapshot_meta "$snap" "$(workdir)" ) 2>&1 )" || rc=$?

  (( rc != 0 )) || fail "снапшот без db.dump и meta.tar принят молча"
  [[ "$out" == *"$snap"* ]] || fail "в ошибке нет пути снапшота: $out"
}

test_empty_dump_is_rejected() {
  local snap rc out
  snap="$TMPROOT/proofreader-20260101T000000Z"
  mkdir -p "$snap/files"
  : > "$snap/db.dump"
  rc=0
  out="$( ( snapshot_meta "$snap" "$(workdir)" ) 2>&1 )" || rc=$?

  (( rc != 0 )) || fail "пустой db.dump принят молча"
  [[ "$out" == *db.dump* ]] || fail "невнятная ошибка на пустом db.dump: $out"
}

test_corrupt_archive_is_rejected() {
  local snap rc out
  snap="$TMPROOT/proofreader-20260101T000000Z"
  mkdir -p "$snap/files"
  printf 'not a tar at all\n' > "$snap/meta.tar"
  rc=0
  out="$( ( snapshot_meta "$snap" "$(workdir)" ) 2>&1 )" || rc=$?

  (( rc != 0 )) || fail "битый meta.tar принят молча"
  [[ "$out" == *meta.tar* ]] || fail "невнятная ошибка на битом архиве: $out"
}

test_archive_without_a_dump_is_rejected() {
  local snap rc out staging
  snap="$TMPROOT/proofreader-20260101T000000Z"
  mkdir -p "$snap/files"
  staging="$(mktemp -d)"
  printf 'bundle\n' > "$staging/repo.bundle"
  tar -C "$staging" -cf "$snap/meta.tar" repo.bundle
  rm -rf "$staging"
  rc=0
  out="$( ( snapshot_meta "$snap" "$(workdir)" ) 2>&1 )" || rc=$?

  (( rc != 0 )) || fail "архив без db.dump принят молча"
  [[ "$out" == *db.dump* ]] || fail "невнятная ошибка на архиве без дампа: $out"
}

run_tests
