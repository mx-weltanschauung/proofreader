#!/usr/bin/env bash
# Режим хранилища (местный SeaweedFS или внешний S3) и адрес ext в mc_run.
# Запуск: ./scripts/tests/storage_mode_test.sh — ни docker, ни сети.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
source "$TESTS_DIR/../lib.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

assert_eq() {
  local what="$1" expected="$2" got="$3"
  [[ "$got" == "$expected" ]] || fail "$what
    ожидалось: $expected
    получено:  $got"
}

is_ext() { if endpoint_is_external "$1"; then echo yes; else echo no; fi; }

test_local_endpoints_are_not_external() {
  assert_eq "пусто" no "$(is_ext "")"
  assert_eq "seaweedfs в compose" no "$(is_ext http://seaweedfs:8333)"
  # Машина разработчика: .env указывает сюда. Сочти мы его внешним —
  # локальные backup.sh/restore.sh начали бы отказывать.
  assert_eq "localhost" no "$(is_ext http://localhost:8333)"
  assert_eq "127.0.0.1" no "$(is_ext http://127.0.0.1:8333)"
  assert_eq "одноразовое окружение verify-restore" no "$(is_ext http://proofreader-verify-seaweedfs:8333)"
}

test_provider_endpoints_are_external() {
  assert_eq "внешний" yes "$(is_ext https://s3.example.org)"
  assert_eq "внешний со слэшем" yes "$(is_ext https://s3.example.org/)"
  assert_eq "чужой хост с портом" yes "$(is_ext http://minio.example.org:9000)"
}

test_mc_host_url_embeds_credentials() {
  assert_eq "адрес" "https://AK1:SK2@s3.example.org" "$(mc_host_url https://s3.example.org AK1 SK2)"
  assert_eq "хвостовой слэш" "https://AK1:SK2@s3.example.org" "$(mc_host_url https://s3.example.org/ AK1 SK2)"
}

# mc_run_args <EXT_S3_ENDPOINT> — аргументы, с которыми mc_run позвал бы docker.
mc_run_args() {
  (
    ensure_mc_image() { :; }
    docker() { printf '%s\n' "$@" > "$TMPROOT/docker-args"; }
    NET=n S3_KEY=k S3_SECRET=s EXT_S3_ENDPOINT="$1" EXT_S3_KEY=AK EXT_S3_SECRET=SK
    mc_run ls ext/proofreader
  )
  cat "$TMPROOT/docker-args"
}

test_mc_run_adds_the_ext_alias_when_configured() {
  local args
  args="$(mc_run_args https://s3.example.org)"
  grep -qxF "MC_HOST_ext=https://AK:SK@s3.example.org" <<<"$args" ||
    fail "нет адреса ext в аргументах docker: $args"
  grep -qxF "MC_HOST_seaweed=http://k:s@seaweedfs:8333" <<<"$args" ||
    fail "адрес seaweed пропал: $args"
}

test_mc_run_has_no_ext_alias_by_default() {
  local args
  args="$(mc_run_args "")"
  if grep -q "MC_HOST_ext" <<<"$args"; then
    fail "адрес ext появился без EXT_S3_ENDPOINT: $args"
  fi
  return 0
}

# load_in <содержимое .env> [VAR=знач ...] — load_config над подставным .env в
# подоболочке; печатает итоговые переменные строкой "S3_ENDPOINT|EXT_S3_ENDPOINT|EXT_S3_KEY|S3_AUDIO_BUCKET".
load_in() {
  local env="$1"; shift
  printf '%s\n' "$env" > "$TMPROOT/.env"
  (
    REPO_ROOT="$TMPROOT"
    for kv in "$@"; do export "${kv?}"; done
    load_config
    printf '%s|%s|%s|%s\n' "$S3_ENDPOINT" "$EXT_S3_ENDPOINT" "$EXT_S3_KEY" "$S3_AUDIO_BUCKET"
  )
}

test_external_storage_fills_the_ext_alias_from_s3_settings() {
  assert_eq "внешнее из .env" "https://s3.example.org|https://s3.example.org|AK|proofreader-audio" \
    "$(load_in $'S3_ENDPOINT=https://s3.example.org\nS3_ACCESS_KEY=AK\nS3_SECRET_KEY=SK')"
}

test_local_storage_leaves_the_ext_alias_empty() {
  assert_eq "местное из .env" "http://localhost:8333|||proofreader-audio" \
    "$(load_in $'S3_ENDPOINT=http://localhost:8333\nS3_ACCESS_KEY=k')"
}

test_caller_environment_beats_dotenv_for_storage() {
  # Переливка на боевом: .env ещё местный, адрес ext приходит из окружения.
  assert_eq "окружение поверх .env" "http://seaweedfs:8333|https://s3.example.org|AK|proofreader-audio" \
    "$(load_in 'S3_ENDPOINT=http://seaweedfs:8333' EXT_S3_ENDPOINT=https://s3.example.org EXT_S3_KEY=AK EXT_S3_SECRET=SK)"
  # Ключа в .env нет — S3_KEY берёт умолчание load_config, им же и EXT_S3_KEY.
  assert_eq "S3_ENDPOINT из окружения" "https://s3.example.org|https://s3.example.org|proofreader_dev|proofreader-audio" \
    "$(load_in 'S3_ENDPOINT=http://localhost:8333' S3_ENDPOINT=https://s3.example.org)"
}

test_refuse_external_storage_dies_only_when_external() {
  local out rc=0
  out="$( S3_ENDPOINT=https://s3.example.org; refuse_external_storage backup.sh 2>&1 )" || rc=$?
  (( rc != 0 )) || fail "при внешнем хранилище отказ обязан ронять скрипт"
  [[ "$out" == *"backup.sh"*"внешнее"* ]] || fail "в отказе нет имени скрипта и причины: $out"
  rc=0
  ( S3_ENDPOINT=http://seaweedfs:8333; refuse_external_storage backup.sh ) || rc=$?
  (( rc == 0 )) || fail "при местном хранилище отказа быть не должно"
}

run_tests
