#!/usr/bin/env bash
# Тесты чистых функций scripts/bootstrap.sh: аргументы, секреты, рендеринг
# конфигов, отметки фаз. Запуск: ./scripts/tests/bootstrap_config_test.sh
# Ни docker, ни сеть не нужны — файловая система и строки.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../bootstrap.sh
source "$TESTS_DIR/../bootstrap.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

assert_eq() {
  local what="$1" expected="$2" got="$3"
  [[ "$got" == "$expected" ]] || fail "$what
    ожидалось: $expected
    получено:  $got"
}

test_s3_domain_defaults_to_the_main_domain_with_a_prefix() {
  parse_args --domain lib.example.org --snapshot "$TMPROOT"
  assert_eq "поддомен хранилища" "s3.lib.example.org" "$S3_DOMAIN"
}

test_s3_domain_can_be_set_explicitly() {
  parse_args --domain lib.example.org --s3-domain scans.example.org --snapshot "$TMPROOT"
  assert_eq "явный поддомен хранилища" "scans.example.org" "$S3_DOMAIN"
}

test_a_domain_that_is_not_a_domain_is_rejected() {
  local out rc=0
  out="$(parse_args --domain 'http://lib.example.org' --snapshot "$TMPROOT" 2>&1)" || rc=$?
  (( rc != 0 )) || fail "адрес со схемой должен отвергаться, а разбор прошёл"
  [[ "$out" == *"домен"* ]] || fail "в сообщении нет слова «домен»: $out"
}

test_secrets_are_hex_only() {
  local s
  s="$(gen_secret 16)"
  [[ "$s" =~ ^[0-9a-f]{32}$ ]] ||
    fail "секрет обязан быть hex длиной 32: получено '$s'
    не-hex ломает mc_run (ключи вшиты в URL) и JSON логина в export.sh"
}

test_env_is_generated_once_and_never_regenerated() {
  local env="$TMPROOT/.env" first second
  DOMAIN=lib.example.org S3_DOMAIN=s3.lib.example.org CONFIG_DIR="$TMPROOT/config"
  write_env "$env"
  first="$(grep '^DB_PASSWORD=' "$env")"
  write_env "$env"
  second="$(grep '^DB_PASSWORD=' "$env")"
  assert_eq "пароль базы при повторном прогоне" "$first" "$second"
}

test_generated_env_is_readable_by_lib_load_config() {
  local env="$TMPROOT/.env"
  DOMAIN=lib.example.org S3_DOMAIN=s3.lib.example.org CONFIG_DIR="$TMPROOT/config"
  write_env "$env"
  # lib.sh делает `set -a; source .env` — файл обязан быть валидным shell.
  ( set -a; source "$env"; set +a ) || fail "сгенерированный .env не источается"
  grep -q '^S3_ACCESS_KEY=' "$env" || fail "нет S3_ACCESS_KEY — restore.sh не найдёт ключи"
  grep -q '^S3_BUCKET=proofreader$' "$env" || fail "нет S3_BUCKET=proofreader"
  grep -q '^DB_USER=proofreader$' "$env" || fail "нет DB_USER=proofreader"
}

test_generated_env_carries_api_url_for_export_sh() {
  local env="$TMPROOT/.env"
  DOMAIN=lib.example.org S3_DOMAIN=s3.lib.example.org CONFIG_DIR="$TMPROOT/config"
  write_env "$env"
  # export.sh берёт API_URL из окружения (load_config его экспортирует); без
  # этой строки на боевом он молча стучался бы в http://localhost:8080.
  grep -q '^API_URL=https://lib.example.org$' "$env" ||
    fail "нет API_URL=https://lib.example.org — export.sh на боевом не найдёт API"
}

test_s3_config_carries_the_same_keys_as_env() {
  local out
  out="$(render_s3_config aaa111 bbb222)"
  [[ "$out" == *'"accessKey": "aaa111"'* ]] || fail "в s3_config.json нет accessKey: $out"
  [[ "$out" == *'"secretKey": "bbb222"'* ]] || fail "в s3_config.json нет secretKey: $out"
}

test_caddyfile_gets_both_site_blocks() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" \
    lib.example.org s3.lib.example.org admin@example.org)"
  [[ "$out" == *"lib.example.org {"* ]] || fail "нет блока основного домена: $out"
  [[ "$out" == *"s3.lib.example.org {"* ]] || fail "нет блока хранилища: $out"
  [[ "$out" == *"email admin@example.org"* ]] || fail "нет адреса для ACME: $out"
  [[ "$out" != *"__"* ]] || fail "в конфиге остались плейсхолдеры: $out"
}

test_caddyfile_without_an_email_drops_the_line() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" \
    lib.example.org s3.lib.example.org "")"
  [[ "$out" != *"email"* ]] || fail "пустой адрес не должен давать строку email: $out"
  [[ "$out" != *"__"* ]] || fail "в конфиге остались плейсхолдеры: $out"
}

# Свой сертификат у основного домена — Origin Certificate от Cloudflare.
# Проверяется в обе стороны: без него Caddy обязан остаться на ACME, иначе
# сервер без Cloudflare перед ним не поднимется вовсе (сослался бы на файлы,
# которых нет), а с ним — обязан перестать ходить в ACME, ради чего всё и
# затевалось.
test_caddyfile_with_an_own_certificate_stops_using_acme() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" \
    lib.example.org s3.lib.example.org admin@example.org yes)"
  [[ "$out" == *"tls /etc/caddy/certs/origin.pem /etc/caddy/certs/origin.key"* ]] ||
    fail "нет строки со своим сертификатом: $out"
  [[ "$out" != *"__"* ]] || fail "в конфиге остались плейсхолдеры: $out"
}

test_caddyfile_without_an_own_certificate_drops_the_tls_line() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" \
    lib.example.org s3.lib.example.org admin@example.org)"
  [[ "$out" != *"tls "* ]] || fail "без сертификата строки tls быть не должно: $out"
  [[ "$out" != *"__"* ]] || fail "в конфиге остались плейсхолдеры: $out"
}

test_a_done_phase_is_skipped() {
  mkdir -p "$TMPROOT/state"
  phase_pending "$TMPROOT/state" clone || fail "невыполненная фаза должна требовать работы"
  touch "$TMPROOT/state/clone.done"
  ! phase_pending "$TMPROOT/state" clone || fail "выполненная фаза должна пропускаться"
}

test_compose_version_is_compared_by_parts() {
  version_ge 2.24.0 2.24.0 || fail "равные версии должны проходить"
  version_ge 5.3.1 2.24.0  || fail "5.3.1 новее 2.24.0"
  # Форма `! x || fail`, а не `x && fail`: под `set -e` неудача первой команды
  # AND-списка делает статусом всего списка единицу, и тест падал бы «до проверок».
  ! version_ge 2.9.0 2.24.0 ||
    fail "2.9.0 старше 2.24.0 — значит сравнение идёт по строке, а не по числам"
}

test_clone_from_bundle_gives_a_working_tree() {
  # Настоящий бандл настоящего репозитория — ровно то, что лежит в meta.tar.
  local repo="$TESTS_DIR/../.."
  git -C "$repo" bundle create "$TMPROOT/repo.bundle" --all >/dev/null 2>&1 ||
    fail "не удалось собрать бандл — дальше проверять нечего"
  clone_from_bundle "$TMPROOT/repo.bundle" "$TMPROOT/app" ||
    fail "клон из бандла не удался"
  [[ -f "$TMPROOT/app/scripts/restore.sh" ]] ||
    fail "в клоне нет scripts/restore.sh — восстанавливать будет нечем"
  [[ -f "$TMPROOT/app/docker-compose.prod.yml" ]] ||
    fail "в клоне нет docker-compose.prod.yml — поднимать будет нечем"
}

test_clone_is_idempotent() {
  local repo="$TESTS_DIR/../.."
  git -C "$repo" bundle create "$TMPROOT/repo.bundle" --all >/dev/null 2>&1 || return 0
  clone_from_bundle "$TMPROOT/repo.bundle" "$TMPROOT/app" || fail "первый клон не удался"
  clone_from_bundle "$TMPROOT/repo.bundle" "$TMPROOT/app" ||
    fail "повторный клон обязан быть безобидным, а он упал"
}

test_a_second_restore_is_skipped_with_a_warning_without_force() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/restore.done"
  FORCE_RESTORE=""
  local out rc=0
  out="$(restore_guard "$TMPROOT/state" 2>&1)" || rc=$?
  (( rc != 0 )) || fail "повторное восстановление без флага должно пропускаться (ненулевой код), а не выполняться"
  [[ "$out" == *"--force-restore"* ]] || fail "в предупреждении не назван выход: $out"
}

test_a_skipped_restore_does_not_abort_the_run() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/restore.done"
  FORCE_RESTORE=""
  local out
  # Метка печатается ПОСЛЕ вызова внутри одной подоболочки: die внутри guard
  # оборвал бы её, и метка не появилась бы. Проверять это кодом возврата
  # бесполезно — $(...) и сам форкает подоболочку, снаружи exit неотличим от return.
  out="$( ( restore_guard "$TMPROOT/state" >/dev/null 2>&1 || true; printf 'продолжили' ) )"
  assert_eq "после пропуска восстановления прогон обязан продолжиться" "продолжили" "$out"
}

test_a_second_restore_is_allowed_with_force() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/restore.done"
  FORCE_RESTORE=1
  restore_guard "$TMPROOT/state" >/dev/null ||
    fail "с --force-restore повторное восстановление должно проходить"
}

test_a_first_restore_proceeds_without_a_marker() {
  mkdir -p "$TMPROOT/state"
  FORCE_RESTORE=""
  restore_guard "$TMPROOT/state" >/dev/null ||
    fail "без отметки restore.done восстановление обязано выполняться и без флага"
}

test_run_phase_skips_by_marker_but_force_overrides() {
  STATE_DIR="$TMPROOT/state"; mkdir -p "$STATE_DIR"
  local calls=0
  _probe() { calls=$((calls + 1)); }
  run_phase probe _probe 2>/dev/null
  (( calls == 1 )) || fail "первый прогон обязан выполнить фазу, вызовов: $calls"
  run_phase probe _probe 2>/dev/null
  (( calls == 1 )) || fail "по отметке фаза обязана пропускаться, вызовов: $calls"
  run_phase probe _probe force 2>/dev/null
  (( calls == 2 )) || fail "с force отметка обязана игнорироваться, вызовов: $calls"
}

test_preview_url_is_unescaped_from_json() {
  local raw='https://s3.lib.example.org/proofreader/works/48/pages/p1.png?X-Amz-Algorithm=AWS4-HMAC-SHA256\u0026X-Amz-Signature=abc\u0026X-Amz-Expires=1800'
  local got
  got="$(unescape_json_url "$raw")"
  [[ "$got" != *'\u0026'* ]] || fail "амперсанды остались экранированными: $got"
  [[ "$got" == *"?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc&X-Amz-Expires=1800" ]] ||
    fail "ссылка распакована неверно: $got"
}

test_domain_guard_passes_when_no_env_exists() {
  domain_guard "$TMPROOT/nope.env" lib.example.org s3.lib.example.org >/dev/null 2>&1 ||
    fail "нет .env — проверять нечего, гвард не должен падать"
}

test_domain_guard_passes_when_domain_matches() {
  printf 'DOMAIN=lib.example.org\nS3_DOMAIN=s3.lib.example.org\n' > "$TMPROOT/.env"
  domain_guard "$TMPROOT/.env" lib.example.org s3.lib.example.org >/dev/null 2>&1 ||
    fail "домен и поддомен хранилища совпадают — гвард не должен падать"
}

test_domain_guard_rejects_a_different_domain() {
  printf 'DOMAIN=old.example.org\nS3_DOMAIN=s3.old.example.org\n' > "$TMPROOT/.env"
  local out rc=0
  out="$(domain_guard "$TMPROOT/.env" new.example.org s3.old.example.org 2>&1)" || rc=$?
  (( rc != 0 )) || fail "другой домен в существующем .env должен отвергаться"
  [[ "$out" == *"config.done"* ]] || fail "в сообщении нет подсказки про config.done: $out"
}

test_domain_guard_rejects_a_different_s3_domain() {
  # Тот же класс тихо устаревшего конфига, что и смена --domain: другой
  # --s3-domain при неизменном --domain раньше проходил бы гвард молча.
  printf 'DOMAIN=lib.example.org\nS3_DOMAIN=old-scans.example.org\n' > "$TMPROOT/.env"
  local out rc=0
  out="$(domain_guard "$TMPROOT/.env" lib.example.org new-scans.example.org 2>&1)" || rc=$?
  (( rc != 0 )) || fail "другой поддомен хранилища в существующем .env должен отвергаться"
  [[ "$out" == *"config.done"* ]] || fail "в сообщении нет подсказки про config.done: $out"
  [[ "$out" == *"old-scans.example.org"* ]] ||
    fail "в сообщении должен называться разошедшийся поддомен хранилища: $out"
}

# Отметки, которые восстановление делает недействительными. Дефект был не
# гипотетический: на боевом lib.example.org admin.done лежал с первого
# разворачивания, и повторный прогон с --force-restore вернул бы туда пароль
# администратора из бэкапа, пропустив его смену по этой отметке.
test_restoring_invalidates_the_admin_password_change() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/admin.done"
  invalidate_post_restore_marks "$TMPROOT/state"
  phase_pending "$TMPROOT/state" admin ||
    fail "восстановление вернуло пароль из бэкапа — смена пароля обязана выполниться заново"
}

test_restoring_invalidates_the_smoke_check() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/smoke.done"
  invalidate_post_restore_marks "$TMPROOT/state"
  phase_pending "$TMPROOT/state" smoke ||
    fail "приёмка проверяла прежние данные — после восстановления она недействительна"
}

test_invalidating_marks_leaves_the_other_phases_alone() {
  mkdir -p "$TMPROOT/state"
  touch "$TMPROOT/state/"{clone,config,infra,app,restore}.done
  invalidate_post_restore_marks "$TMPROOT/state"
  local name
  for name in clone config infra app restore; do
    ! phase_pending "$TMPROOT/state" "$name" ||
      fail "фаза $name восстановлением не отменяется — её отметку трогать нельзя"
  done
}

test_invalidating_marks_is_safe_on_a_first_run() {
  mkdir -p "$TMPROOT/state"
  invalidate_post_restore_marks "$TMPROOT/state" ||
    fail "на первом прогоне отметок ещё нет — снятие несуществующих не должно падать"
}

# Глобалы режима переживают тест; каждый внешний тест сбрасывает их в начале и
# в конце, чтобы соседние (местные) тесты не унаследовали внешний режим.
reset_mode() { EXTERNAL_S3=""; S3_REGION_ARG="us-east-1"; S3_KEY_ARG=""; S3_SECRET_ARG=""; }

external_args() {
  parse_args --domain lib.example.org --snapshot "$TMPROOT" \
    --external-s3 https://s3.example.org --s3-region default --s3-key AK1 --s3-secret SK2
}

test_external_s3_needs_no_storage_subdomain() {
  reset_mode
  external_args
  assert_eq "внешний адрес" "https://s3.example.org" "$EXTERNAL_S3"
  assert_eq "поддомен хранилища" "" "$S3_DOMAIN"
  reset_mode
}

test_external_s3_rejects_bad_input() {
  reset_mode
  local rc
  rc=0; ( parse_args --domain lib.example.org --snapshot "$TMPROOT" --external-s3 https://s3.example.org ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "без ключей внешний режим обязан отказать"
  rc=0; ( parse_args --domain lib.example.org --snapshot "$TMPROOT" --external-s3 http://s3.example.org --s3-key A --s3-secret B ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "адрес без https обязан отвергаться"
  rc=0; ( parse_args --domain lib.example.org --snapshot "$TMPROOT" --external-s3 https://s3.example.org --s3-key 'A@b' --s3-secret B ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "ключ с не-буквенно-цифровыми знаками обязан отвергаться (mc_run вшивает его в URL)"
  rc=0; ( parse_args --domain lib.example.org --snapshot "$TMPROOT" --external-s3 https://s3.example.org --s3-key A --s3-secret B --s3-domain s3.lib.example.org ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "--s3-domain при внешнем хранилище не нужен и обязан отвергаться"
  reset_mode
}

test_external_env_points_both_endpoints_at_the_provider() {
  reset_mode
  local env="$TMPROOT/.env"
  external_args
  CONFIG_DIR="$TMPROOT/config"
  write_env "$env"
  grep -qx 'S3_ENDPOINT=https://s3.example.org' "$env" || fail "S3_ENDPOINT не внешний: $(grep ^S3_ "$env")"
  grep -qx 'S3_PUBLIC_ENDPOINT=https://s3.example.org' "$env" || fail "S3_PUBLIC_ENDPOINT не внешний"
  grep -qx 'S3_REGION=default' "$env" || fail "регион не из --s3-region"
  grep -qx 'S3_ACCESS_KEY=AK1' "$env" || fail "ключ не из --s3-key"
  grep -qx 'S3_SECRET_KEY=SK2' "$env" || fail "секрет не из --s3-secret"
  grep -qx 'S3_CORS_ORIGINS=https://lib.example.org' "$env" || fail "CORS не открыт домену читальни"
  if grep -q '^COMPOSE_PROFILES=\|^S3_DOMAIN=' "$env"; then
    fail "внешнему режиму не нужны ни профиль SeaweedFS, ни поддомен: $(grep '^COMPOSE_PROFILES=\|^S3_DOMAIN=' "$env")"
  fi
  reset_mode
}

test_local_env_enables_the_seaweedfs_profile() {
  reset_mode
  local env="$TMPROOT/.env"
  DOMAIN=lib.example.org S3_DOMAIN=s3.lib.example.org CONFIG_DIR="$TMPROOT/config"
  write_env "$env"
  grep -qx 'COMPOSE_PROFILES=local-storage' "$env" || fail "местный режим обязан включать профиль seaweedfs"
  grep -qx 'S3_ENDPOINT=http://seaweedfs:8333' "$env" || fail "местный S3_ENDPOINT потерян"
}

test_caddyfile_without_storage_subdomain_has_no_storage_block() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" lib.example.org "" admin@example.org)"
  [[ "$out" == *"lib.example.org {"* ]] || fail "нет блока основного домена: $out"
  [[ "$out" != *"seaweedfs"* ]] || fail "блок хранилища остался: $out"
  [[ "$out" != *"S3-BLOCK"* && "$out" != *"__"* ]] || fail "остались маркеры или плейсхолдеры: $out"
}

test_caddyfile_with_storage_subdomain_drops_only_the_markers() {
  local out
  out="$(render_caddyfile "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" lib.example.org s3.lib.example.org admin@example.org)"
  [[ "$out" == *"s3.lib.example.org {"* && "$out" == *"reverse_proxy seaweedfs:8333"* ]] ||
    fail "блок хранилища пропал: $out"
  [[ "$out" != *"S3-BLOCK"* ]] || fail "маркеры блока остались в файле: $out"
}

test_domain_guard_rejects_a_storage_mode_switch() {
  reset_mode
  local rc
  printf 'DOMAIN=lib.example.org\nS3_DOMAIN=s3.lib.example.org\nS3_ENDPOINT=http://seaweedfs:8333\n' > "$TMPROOT/.env"
  EXTERNAL_S3=https://s3.example.org
  rc=0; ( domain_guard "$TMPROOT/.env" lib.example.org "" ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "местный .env + внешний прогон обязан отказать"
  printf 'DOMAIN=lib.example.org\nS3_ENDPOINT=https://s3.example.org\n' > "$TMPROOT/.env"
  EXTERNAL_S3=""
  rc=0; ( domain_guard "$TMPROOT/.env" lib.example.org s3.lib.example.org ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "внешний .env + местный прогон обязан отказать"
  reset_mode
}

# Живой .env боевого написан прежним bootstrap и несёт S3_DOMAIN поддомена
# SeaweedFS; после переезда (спека 2026-10-02-external-s3-storage) S3_ENDPOINT
# в нём уже внешний, а у внешнего прогона S3_DOMAIN пуст. Сравнивать поддомен
# тут не с чем: смену режима ловит проверка S3_ENDPOINT, а «другой поддомен
# хранилища» в отказе вводил бы оператора в заблуждение.
test_domain_guard_ignores_a_stale_s3_domain_in_external_mode() {
  reset_mode
  printf 'DOMAIN=lib.example.org\nS3_DOMAIN=s3.lib.example.org\nS3_ENDPOINT=https://s3.example.org\n' > "$TMPROOT/.env"
  local out rc=0
  out="$(EXTERNAL_S3=https://s3.example.org; domain_guard "$TMPROOT/.env" lib.example.org "" 2>&1)" || rc=$?
  (( rc == 0 )) || fail "внешний .env с прежним S3_DOMAIN + внешний прогон обязан проходить: $out"
  # Другой основной домен по-прежнему отвергается и во внешнем режиме.
  rc=0
  out="$(EXTERNAL_S3=https://s3.example.org; domain_guard "$TMPROOT/.env" new.example.org "" 2>&1)" || rc=$?
  (( rc != 0 )) || fail "другой домен обязан отвергаться и во внешнем режиме"
  reset_mode
}

# Снапшот, снятый до поддержки внешнего хранилища, несёт compose, требующий
# S3_DOMAIN, и restore.sh без --db-only: внешний прогон на нём падал поздно, на
# сборке образов, и не тем словом. Отказ — в начале phase_config, по признаку
# --db-only в restore.sh клона.
fake_app_for_config() {
  APP_DIR="$TMPROOT/app"; CONFIG_DIR="$TMPROOT/config"
  mkdir -p "$APP_DIR/scripts" "$APP_DIR/docker/caddy"
  cp "$TESTS_DIR/../../docker/caddy/Caddyfile.tmpl" "$APP_DIR/docker/caddy/"
  printf '#!/usr/bin/env bash\n%s\n' "$1" > "$APP_DIR/scripts/restore.sh"
}

test_external_config_refuses_a_snapshot_without_db_only_restore() {
  reset_mode
  local out rc=0
  out="$(
    external_args
    fake_app_for_config 'case "$1" in --yes) ;; esac  # старый restore.sh'
    phase_config 2>&1
  )" || rc=$?
  (( rc != 0 )) || fail "старый снапшот во внешнем режиме обязан отказать в phase_config"
  [[ "$out" == *"--db-only"* ]] || fail "отказ обязан назвать причину (нет --db-only): $out"
  [[ ! -e "$TMPROOT/app/.env" ]] || fail "отказ обязан прийти до записи конфигов: .env уже записан"
  reset_mode
}

test_external_config_accepts_a_snapshot_with_db_only_restore() {
  reset_mode
  local out rc=0
  out="$(
    external_args
    fake_app_for_config 'case "$1" in --db-only) ;; esac'
    phase_config 2>&1
  )" || rc=$?
  (( rc == 0 )) || fail "снапшот с --db-only в restore.sh обязан проходить: $out"
  [[ -s "$TMPROOT/config/Caddyfile" ]] || fail "Caddyfile не записан"
  reset_mode
}

test_local_config_does_not_need_db_only_restore() {
  reset_mode
  local out rc=0
  out="$(
    DOMAIN=lib.example.org S3_DOMAIN=s3.lib.example.org ACME_EMAIL=""
    fake_app_for_config 'echo старый restore.sh'
    phase_config 2>&1
  )" || rc=$?
  (( rc == 0 )) || fail "местному режиму --db-only в restore.sh не нужен: $out"
  reset_mode
}

# Фазы зовутся в подоболочке: подставные compose/wait_ready и APP_DIR/ROOT_DIR
# не должны пережить тест. Вывод — в файлах, проверки — снаружи.
test_external_phases_skip_seaweedfs() {
  reset_mode
  (
    EXTERNAL_S3=https://s3.example.org
    APP_DIR="$TMPROOT/app"; ROOT_DIR="$TMPROOT"; SNAPSHOT_DIR="$TMPROOT/snap"
    mkdir -p "$APP_DIR/scripts"
    printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" > "%s/restore-args"\n' "$TMPROOT" > "$APP_DIR/scripts/restore.sh"
    chmod +x "$APP_DIR/scripts/restore.sh"
    compose() { printf '%s\n' "$*" >> "$TMPROOT/compose.log"; }
    wait_ready() { printf 'wait %s\n' "$1" >> "$TMPROOT/compose.log"; }
    phase_infra
    phase_restore
    phase_app
  ) >/dev/null 2>&1 || { fail "фазы внешнего режима упали"; return 0; }
  if grep -q seaweedfs "$TMPROOT/compose.log"; then
    fail "внешний режим не поднимает и не ждёт seaweedfs: $(cat "$TMPROOT/compose.log")"
  fi
  grep -q -- '--db-only' "$TMPROOT/restore-args" || fail "восстановление обязано быть только базой: $(cat "$TMPROOT/restore-args")"
  reset_mode
}

test_external_preflight_accepts_a_snapshot_without_files() {
  reset_mode
  mkdir -p "$TMPROOT/snap"; printf '{}' > "$TMPROOT/snap/manifest.json"; printf 'x' > "$TMPROOT/snap/meta.tar"
  local rc=0
  (
    EXTERNAL_S3=https://s3.example.org
    ROOT_DIR="$TMPROOT"; SNAPSHOT_DIR="$TMPROOT/snap"
    ss() { :; }
    # /tmp бывает tmpfs меньше 4 ГБ — место подставное, проверяется не оно.
    df() { printf 'Filesystem 1K-blocks Used Available Use%% Mounted\nx 1 1 99999999 1%% /\n'; }
    phase_preflight
  ) >/dev/null 2>&1 || rc=$?
  (( rc == 0 )) || fail "снапшот без files/ во внешнем режиме законен"
}

run_tests
