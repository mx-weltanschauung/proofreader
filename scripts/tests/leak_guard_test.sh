#!/usr/bin/env bash
# Сторож утечек (scripts/leak-guard.sh) на подложном репозитории: что он
# обязан ловить сам, что — только по списку экземпляра, и что не трогать.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$TESTS_DIR/../leak-guard.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

# repo — git-репозиторий с файлами разной степени опасности.
repo() {
  # Поддельный ключ собирается из двух половин: целиком в исходнике он сам
  # стал бы находкой сканеров секретов (gitleaks) у публичного репозитория.
  local key="AKIA""ABCDEFGHIJKLMNOP"
  git init -q "$TMPROOT/r"
  cd "$TMPROOT/r"
  printf 'base := "https://lib.example.org"\nmail := "x@example.org"\n' >ok.go
  printf 'key := "%s"\n' "$key" >key.go
  printf 'сервер 8.8.8.8, он же 8.090.8.8\n' >ip.txt
  printf 'пишите a.b@gmail.com\n' >mail.md
  printf 'документационные 192.0.2.1 10.0.0.1 127.0.0.1 0.0.0.0 версия 1.2.3.4.5 сеть 173.245.48.0/20 не адрес 999.1.2.3\n' >private_ip.txt
  printf 'здесь секретное-слово\n' >deny.txt
  mkdir -p vendor && printf 'key := "%s"\n' "$key" >vendor/dep.go
  printf 'untracked %s\n' "$key" >untracked.txt
  git add ok.go key.go ip.txt mail.md private_ip.txt deny.txt vendor/dep.go
  cd - >/dev/null
}

findings() { cut -d: -f1 <<<"$1" | sort -u | tr '\n' ' '; }

test_generic_patterns_without_denylist() {
  repo
  local out rc=0
  out="$(LEAK_DENYLIST= "$GUARD" "$TMPROOT/r" 2>"$TMPROOT/err")" || rc=$?
  [[ ! -s "$TMPROOT/err" ]] || fail "сторож писал в stderr: $(cat "$TMPROOT/err")"
  [[ $rc -eq 1 ]] || fail "находки есть, а код $rc"
  [[ "$(findings "$out")" == "ip.txt key.go mail.md " ]] ||
    fail "без списка ждали ip.txt key.go mail.md, получили: $(findings "$out")"$'\n'"$out"
}

test_denylist_adds_instance_words() {
  repo
  printf 'секретное-слово\n\n' >"$TMPROOT/deny.list"
  local out
  out="$(LEAK_DENYLIST="$TMPROOT/deny.list" "$GUARD" "$TMPROOT/r" || true)"
  [[ "$(findings "$out")" == "deny.txt ip.txt key.go mail.md " ]] ||
    fail "со списком ждали ещё deny.txt, получили: $(findings "$out")"
}

test_clean_tree_passes() {
  git init -q "$TMPROOT/c"
  printf 'https://lib.example.org\n' >"$TMPROOT/c/a.txt"
  git -C "$TMPROOT/c" add a.txt
  local rc=0
  LEAK_DENYLIST= "$GUARD" "$TMPROOT/c" >/dev/null || rc=$?
  [[ $rc -eq 0 ]] || fail "чистое дерево: код $rc"
}

test_missing_denylist_file_is_an_error() {
  repo
  local rc=0
  LEAK_DENYLIST="$TMPROOT/nope" "$GUARD" "$TMPROOT/r" >/dev/null 2>&1 || rc=$?
  [[ $rc -eq 2 ]] || fail "список задан, а файла нет — ждали код 2, получили $rc"
}

run_tests
