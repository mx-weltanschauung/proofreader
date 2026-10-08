#!/usr/bin/env bash
# Общая обвязка для тестов скриптов из scripts/. Источается, не исполняется.
# Каждый тест — функция test_*; run_tests запускает их в своём пустом $TMPROOT.

FAILURES=0
TMPROOT=""

# На GitHub Actions: скрипт, умерший мимо fail() (exit внутри проверяемой
# функции, die под set -e), иначе оставляет на странице прогона только
# «exit code 1». Аннотация называет команду и цепочку функций.
if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
  trap 'harness_rc=$?; if (( harness_rc != 0 )); then printf "::error title=%s::выход %s на «%s» в %s\n" "$(basename "$0")" "$harness_rc" "$BASH_COMMAND" "${FUNCNAME[*]:-верхний уровень}"; fi' EXIT
fi

fail() {
  printf '  FAIL: %s\n' "$*" >&2
  # На GitHub Actions — ещё и аннотацией: она видна на странице прогона без
  # доступа к журналу задачи. Переводы строк — в %0A, иначе аннотация рвётся.
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    local msg="$*"
    printf '::error title=%s::%s\n' "$(basename "$0")" "${msg//$'\n'/%0A}"
  fi
  FAILURES=$((FAILURES + 1))
  return 1
}

# snapshot <name> [--incomplete] — каталог снапшота с правдоподобным содержимым
snapshot() {
  local name="$1" flag="${2:-}"
  mkdir -p "$TMPROOT/$name/files"
  printf 'dump\n' > "$TMPROOT/$name/db.dump"
  [[ "$flag" == "--incomplete" ]] && touch "$TMPROOT/$name/.INCOMPLETE"
  return 0
}

# entries [dir] — что лежит в каталоге, по одному имени в строке, отсортировано
entries() { (cd "${1:-$TMPROOT}" && ls -A | sort); }

assert_entries() {
  local expected got
  expected="$(printf '%s\n' "$@" | sort)"
  got="$(entries)"
  if [[ "$expected" != "$got" ]]; then
    fail "содержимое каталога расходится
    ожидалось:
$(printf '%s\n' "$expected" | sed 's/^/      /')
    получено:
$(printf '%s\n' "$got" | sed 's/^/      /')"
  fi
}

run_tests() {
  local t before rc
  for t in $(declare -F | awk '{print $3}' | grep '^test_' | sort); do
    TMPROOT="$(mktemp -d)"
    printf '%s\n' "$t"
    before="$FAILURES"
    rc=0
    # set +e вместо "$t" || rc=$?: bash отключает errexit не только для "$t"
    # само по себе, но и для ЛЮБОГО set -e, который "$t" (или что-то глубже
    # внутри него, например подоболочка тестового харнесса) попытается
    # включить заново, — эффект держится, пока "$t" не завершится целиком.
    # Явный set +e/set -e — реальное переключение опции, а не позиция в
    # ||-списке, поэтому вложенный set -e внутри "$t" (см.
    # backup_prod_run_test.sh:run_main) работает по-настоящему. -e
    # сознательно не восстанавливается после "$t": он и раньше был
    # неэффективен внутри тестовых функций (та же причина), так что для
    # существующих тестов это не меняет поведения; единственное место, где
    # это должно иметь эффект, — там, где тест сам явно попросит -e назад.
    set +e
    "$t"
    rc=$?
    if (( rc != 0 && FAILURES == before )); then
      fail "тест завершился с кодом $rc до проверок" || true
    fi
    rm -rf "$TMPROOT"
  done
  if (( FAILURES > 0 )); then
    printf '\n%d assertion(s) failed\n' "$FAILURES" >&2
    return 1
  fi
  printf '\nall tests passed\n'
}
