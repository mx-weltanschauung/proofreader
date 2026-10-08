#!/usr/bin/env bash
# Тесты чистки в самом backup.sh. Запуск: ./scripts/tests/backup_prune_test.sh
# Настоящий docker не нужен: скрипт гоняется в подставном REPO_ROOT
# (копия scripts/) с заглушкой docker в PATH, так что рабочие backups/ и
# контейнеры не затрагиваются.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REAL_SCRIPTS="$TESTS_DIR/.."
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

# fake_repo — копия scripts/ в $TMPROOT + заглушка docker; печатает путь к backups/
fake_repo() {
  mkdir -p "$TMPROOT/scripts" "$TMPROOT/bin" "$TMPROOT/backups"
  cp "$REAL_SCRIPTS/lib.sh" "$REAL_SCRIPTS/backup.sh" "$TMPROOT/scripts/"
  cat > "$TMPROOT/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
case "${1:-}" in
  inspect)
    case "${3:-}" in
      '{{.State.Running}}') echo true ;;
      '{{.Config.Image}}') echo stub/image:1 ;;
      *) echo unknown ;;
    esac ;;
  exec)
    for a in "$@"; do
      case "$a" in
        pg_dump)
          if [[ -n "${STUB_DUMP_FAILS:-}" ]]; then echo 'pg_dump: boom' >&2; exit 1; fi
          printf 'PGDMP-stub-dump\n'; exit 0 ;;
        psql) printf '000008\n'; exit 0 ;;
      esac
    done ;;
  run)
    for a in "$@"; do
      case "$a" in
        mirror) exit 0 ;;
        ls) printf 'obj-a\nobj-b\n'; exit 0 ;;
      esac
    done ;;
esac
exit 0
STUB
  chmod +x "$TMPROOT/bin/docker"
  echo "$TMPROOT/backups"
}

# bsnapshot <name> [--incomplete] — снапшот внутри backups/ подставного репозитория
bsnapshot() {
  local name="$1" flag="${2:-}"
  mkdir -p "$TMPROOT/backups/$name/files"
  printf 'dump\n' > "$TMPROOT/backups/$name/db.dump"
  [[ "$flag" == "--incomplete" ]] && touch "$TMPROOT/backups/$name/.INCOMPLETE"
  return 0
}

# run_backup [env-assignments...] — вернуть имя созданного снапшота
run_backup() {
  local dest rc=0
  dest="$(env "$@" PATH="$TMPROOT/bin:$PATH" "$TMPROOT/scripts/backup.sh" 2>"$TMPROOT/backup.log")" || rc=$?
  (( rc == 0 )) || return "$rc"
  basename "$dest"
}

assert_backups() {
  local expected got
  expected="$(printf '%s\n' "$@" | sort)"
  got="$(entries "$TMPROOT/backups")"
  if [[ "$expected" != "$got" ]]; then
    fail "содержимое backups/ расходится
    ожидалось:
$(printf '%s\n' "$expected" | sed 's/^/      /')
    получено:
$(printf '%s\n' "$got" | sed 's/^/      /')"
  fi
}

test_backup_keeps_three_snapshots_by_default() {
  fake_repo >/dev/null
  bsnapshot proofreader-20260101T000000Z
  bsnapshot proofreader-20260102T000000Z
  bsnapshot proofreader-20260103T000000Z

  local fresh
  fresh="$(run_backup)"

  assert_backups \
    proofreader-20260102T000000Z \
    proofreader-20260103T000000Z \
    "$fresh"
}

test_backup_keep_env_overrides_the_limit() {
  fake_repo >/dev/null
  bsnapshot proofreader-20260101T000000Z
  bsnapshot proofreader-20260102T000000Z
  bsnapshot proofreader-20260103T000000Z

  local fresh
  fresh="$(run_backup BACKUP_KEEP=5)"

  assert_backups \
    proofreader-20260101T000000Z \
    proofreader-20260102T000000Z \
    proofreader-20260103T000000Z \
    "$fresh"
}

test_failed_backup_does_not_prune_anything() {
  fake_repo >/dev/null
  bsnapshot proofreader-20260101T000000Z
  bsnapshot proofreader-20260102T000000Z
  bsnapshot proofreader-20260103T000000Z

  local rc=0
  run_backup STUB_DUMP_FAILS=1 >/dev/null || rc=$?
  (( rc != 0 )) || fail "упавший бэкап вернул нулевой код"

  # три старых снапшота целы, от упавшего остался помеченный каталог
  local got count
  got="$(entries "$TMPROOT/backups")"
  count="$(printf '%s\n' "$got" | wc -l)"
  (( count == 4 )) || fail "ожидалось 4 каталога (3 старых + битый), получено $count:
$got"
  for name in proofreader-20260101T000000Z proofreader-20260102T000000Z proofreader-20260103T000000Z; do
    [[ -d "$TMPROOT/backups/$name" ]] || fail "старый снапшот $name снесён упавшим бэкапом"
  done
}

run_tests
