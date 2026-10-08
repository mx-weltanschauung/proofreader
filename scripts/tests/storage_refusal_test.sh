#!/usr/bin/env bash
# Скрипты, умеющие только местный SeaweedFS, отказывают при внешнем хранилище
# первым делом — до единого вызова docker. restore.sh --db-only — исключение.
# docker подставной (в $PATH), его вызовы — в $TMPROOT/calls.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"
SCRIPTS="$(cd "$TESTS_DIR/.." && pwd)"

fake_docker() {
  mkdir -p "$TMPROOT/bin"
  cat > "$TMPROOT/bin/docker" <<FAKE
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$TMPROOT/calls"
case "\$1" in inspect) echo true ;; esac
exit 0
FAKE
  chmod +x "$TMPROOT/bin/docker"
}

# run_script <скрипт> [args...] — при внешнем хранилище; код возврата скрипта.
run_external() {
  fake_docker
  S3_ENDPOINT=https://s3.example.org PATH="$TMPROOT/bin:$PATH" \
    bash "$SCRIPTS/$1" "${@:2}" >/dev/null 2>"$TMPROOT/stderr"
}

assert_refused_before_docker() {
  local name="$1" rc="$2"
  (( rc != 0 )) || fail "$name при внешнем хранилище обязан отказать"
  grep -q 'внешнее' "$TMPROOT/stderr" || fail "$name: в отказе нет причины: $(cat "$TMPROOT/stderr")"
  [[ ! -s "$TMPROOT/calls" ]] || fail "$name: отказ обязан прийти до docker, а были вызовы: $(cat "$TMPROOT/calls")"
}

test_backup_refuses_external_storage() {
  local rc=0; run_external backup.sh || rc=$?
  assert_refused_before_docker backup.sh "$rc"
}

test_reset_refuses_external_storage() {
  local rc=0; run_external reset.sh --yes || rc=$?
  assert_refused_before_docker reset.sh "$rc"
}

test_verify_restore_refuses_external_storage() {
  local rc=0; run_external verify-restore.sh || rc=$?
  assert_refused_before_docker verify-restore.sh "$rc"
}

test_restore_refuses_external_storage_without_db_only() {
  mkdir -p "$TMPROOT/snap/files"; printf 'dump' > "$TMPROOT/snap/db.dump"
  local rc=0; run_external restore.sh "$TMPROOT/snap" --yes || rc=$?
  assert_refused_before_docker restore.sh "$rc"
}

test_restore_db_only_restores_the_database_and_nothing_else() {
  # Снапшот без files/ — так его везёт deploy.sh --external-s3.
  mkdir -p "$TMPROOT/snap"; printf 'dump' > "$TMPROOT/snap/db.dump"
  local rc=0; run_external restore.sh "$TMPROOT/snap" --yes --db-only || rc=$?
  (( rc == 0 )) || { fail "restore.sh --db-only упал: $(cat "$TMPROOT/stderr")"; return 0; }
  grep -q 'pg_restore' "$TMPROOT/calls" || fail "база не восстанавливалась: $(cat "$TMPROOT/calls")"
  if grep -q '^run ' "$TMPROOT/calls"; then
    fail "--db-only не должен звать mc (docker run): $(cat "$TMPROOT/calls")"
  fi
  if grep -q 'seaweedfs' "$TMPROOT/calls"; then
    fail "--db-only не должен трогать контейнер SeaweedFS: $(cat "$TMPROOT/calls")"
  fi
  return 0
}

run_tests
