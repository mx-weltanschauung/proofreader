#!/usr/bin/env bash
# Тесты разбора аргументов и сверки DNS в scripts/deploy.sh.
# Запуск: ./scripts/tests/deploy_args_test.sh — ни ssh, ни сети не нужно.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../deploy.sh
source "$TESTS_DIR/../deploy.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

assert_eq() {
  local what="$1" expected="$2" got="$3"
  [[ "$got" == "$expected" ]] || fail "$what
    ожидалось: $expected
    получено:  $got"
}

test_target_and_domain_are_parsed() {
  deploy_parse_args root@203.0.113.4 --domain lib.example.org
  assert_eq "цель" "root@203.0.113.4" "$TARGET"
  assert_eq "домен" "lib.example.org" "$DOMAIN"
  assert_eq "поддомен хранилища" "s3.lib.example.org" "$S3_DOMAIN"
}

# Проверки отказов идут в подоболочке `( ... )` намеренно: die из lib.sh делает
# exit 1, и без неё он завершил бы весь файл тестов, а не отдельный тест —
# набор выглядел бы прошедшим, не выполнив остальных проверок.
test_a_target_without_a_user_is_rejected() {
  local rc=0
  ( deploy_parse_args 203.0.113.4 --domain lib.example.org ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "цель без пользователя должна отвергаться: bootstrap требует root"
}

test_a_missing_domain_is_rejected() {
  local rc=0
  ( deploy_parse_args root@203.0.113.4 ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "без --domain разворачивать нечего"
}

test_dns_matches_when_the_address_is_among_the_answers() {
  dns_matches 203.0.113.4 "$(printf '203.0.113.4\n')" || fail "точное совпадение должно проходить"
  dns_matches 203.0.113.4 "$(printf '198.51.100.8\n203.0.113.4\n')" ||
    fail "совпадение среди нескольких A-записей должно проходить"
}

test_dns_does_not_match_a_different_address() {
  ! dns_matches 203.0.113.4 "$(printf '198.51.100.8\n')" ||
    fail "чужой адрес не должен считаться совпадением: сертификат не выпишется"
  ! dns_matches 203.0.113.4 "" ||
    fail "пустой ответ не должен считаться совпадением: записи нет"
}

test_dns_does_not_match_a_prefix() {
  ! dns_matches 203.0.113.4 "$(printf '203.0.113.40\n')" ||
    fail "203.0.113.40 не равно 203.0.113.4 — сверка обязана быть построчной, а не подстрокой"
}

test_shell_quoting_survives_an_apostrophe() {
  local quoted back
  quoted="$(shq "it's")"
  back="$(eval "printf '%s' $quoted")"
  assert_eq "значение после подстановки и разбора" "it's" "$back"
}

test_shell_quoting_leaves_a_plain_value_intact() {
  local quoted back
  quoted="$(shq "lib.example.org")"
  back="$(eval "printf '%s' $quoted")"
  assert_eq "обычное значение не портится" "lib.example.org" "$back"
}

test_cleanup_tail_does_nothing_without_a_pid() {
  local out rc=0
  unset TAILLOG_PID
  out="$( ( cleanup_tail; printf 'жив' ) )" || rc=$?
  (( rc == 0 )) || fail "уборка без pid обязана быть безобидной, код возврата: $rc"
  [[ "$out" == "жив" ]] || fail "уборка без pid не должна убивать вызывающего, получено: '$out'"
}

test_external_s3_needs_no_storage_subdomain() {
  deploy_parse_args root@203.0.113.4 --domain lib.example.org --external-s3
  assert_eq "внешний режим" "1" "$EXTERNAL_S3"
  assert_eq "поддомен хранилища" "" "$S3_DOMAIN"
  EXTERNAL_S3=""
}

test_external_s3_rejects_a_storage_subdomain() {
  local rc=0
  ( deploy_parse_args root@203.0.113.4 --domain lib.example.org --external-s3 --s3-domain s3.lib.example.org ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "--s3-domain при --external-s3 обязан отвергаться"
}

test_external_run_script_passes_the_provider_to_bootstrap() {
  deploy_parse_args root@203.0.113.4 --domain lib.example.org --external-s3
  EXTERNAL_S3_ENDPOINT=https://s3.example.org EXTERNAL_S3_REGION=default EXTERNAL_S3_ACCESS_KEY=AK1 EXTERNAL_S3_SECRET_KEY=SK2
  local s
  s="$(deploy_run_script)"
  [[ "$s" == *"--external-s3 'https://s3.example.org'"* ]] || fail "нет адреса хранилища: $s"
  [[ "$s" == *"--s3-region 'default'"* && "$s" == *"--s3-key 'AK1'"* && "$s" == *"--s3-secret 'SK2'"* ]] ||
    fail "нет региона или ключей: $s"
  [[ "$s" != *"--s3-domain"* ]] || fail "поддомену хранилища тут не место: $s"
  EXTERNAL_S3=""
}

test_local_run_script_keeps_the_storage_subdomain() {
  deploy_parse_args root@203.0.113.4 --domain lib.example.org
  local s
  s="$(deploy_run_script)"
  [[ "$s" == *"--s3-domain 's3.lib.example.org'"* ]] || fail "нет поддомена хранилища: $s"
  [[ "$s" != *"--external-s3"* ]] || fail "местный режим не должен звать внешний: $s"
}

test_external_dns_check_skips_the_storage_subdomain() {
  deploy_parse_args root@203.0.113.4 --domain lib.example.org --external-s3
  dig() { printf '%s\n' "${*: -1}" >> "$TMPROOT/dig.log"; printf '203.0.113.4\n'; }
  check_dns 203.0.113.4 2>/dev/null
  assert_eq "сверяемые имена" "lib.example.org" "$(cat "$TMPROOT/dig.log")"
  # $(…) глотает хвостовые переводы строки, и пустой запрос dig прошёл бы
  # незамеченным — поэтому число обращений считается отдельно.
  assert_eq "число обращений к dig" "1" "$(wc -l < "$TMPROOT/dig.log" | tr -d ' ')"
  unset -f dig
  EXTERNAL_S3=""
}

run_tests
