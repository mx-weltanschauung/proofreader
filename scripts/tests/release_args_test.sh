#!/usr/bin/env bash
# Тесты разбора аргументов и генерации удалённого скрипта в scripts/release.sh.
# Запуск: ./scripts/tests/release_args_test.sh — ни ssh, ни сети не нужно.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../release.sh
source "$TESTS_DIR/../release.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

assert_eq() {
  local what="$1" expected="$2" got="$3"
  [[ "$got" == "$expected" ]] || fail "$what
    ожидалось: $expected
    получено:  $got"
}

test_target_is_parsed_and_defaults_are_sane() {
  release_parse_args root@203.0.113.4
  assert_eq "цель" "root@203.0.113.4" "$TARGET"
  assert_eq "ref по умолчанию" "HEAD" "$REF"
  # Снимок по умолчанию НЕ снимается: на боевом он копирует сканы целиком, а
  # диск их второй раз не вмещает. Нужен — просят явно, флагом --with-backup.
  assert_eq "бэкап по умолчанию выключен" "" "$DO_BACKUP"
}

# Тот же довод, что в deploy.sh: на сервере скрипт правит /opt и дёргает docker.
test_a_target_without_a_user_is_rejected() {
  local rc=0
  ( release_parse_args 203.0.113.4 ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "цель без пользователя должна отвергаться"
}

test_a_missing_target_is_rejected() {
  local rc=0
  ( release_parse_args --ref main ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "без цели выкатывать некуда"
}

test_with_backup_flag_turns_the_snapshot_on() {
  release_parse_args root@203.0.113.4 --with-backup
  assert_eq "бэкап включён флагом" "1" "$DO_BACKUP"
}

# --no-backup остаётся принимаемым: он стоит в прежних заметках, скиллах и
# привычке рук. Теперь это не выключатель, а подтверждение умолчания, и
# отвергать его значило бы ронять выкатку на безобидном флаге.
test_no_backup_flag_is_still_accepted() {
  release_parse_args root@203.0.113.4 --no-backup
  assert_eq "бэкап выключен" "" "$DO_BACKUP"
}

test_remote_script_pins_the_exact_commit() {
  local script
  script="$(release_remote_script abc1234 1)"
  # Не "git checkout" впритык: остальные git-команды в удалённом скрипте идут
  # через "git -C \"\$APP\"" (без cd — так надёжнее), и эта тоже.
  grep -q 'checkout --detach abc1234' <<< "$script" ||
    fail "удалённый скрипт должен переключаться на переданный коммит, а не на ветку"
}

# Бэкап перед миграцией — единственное, что стоит между опечаткой в .down.sql
# и потерянным корпусом.
test_remote_script_backs_up_before_migrating() {
  local script backup_line migrate_line
  script="$(release_remote_script abc1234 1)"
  backup_line="$(grep -n 'backup.sh' <<< "$script" | head -1 | cut -d: -f1)"
  migrate_line="$(grep -n 'up -d .*migrate' <<< "$script" | head -1 | cut -d: -f1)"
  [[ -n "$backup_line" && -n "$migrate_line" ]] || fail "в скрипте нет бэкапа или миграции"
  (( backup_line < migrate_line )) || fail "бэкап обязан идти до миграции"
}

test_remote_script_can_skip_the_backup() {
  local script
  script="$(release_remote_script abc1234 "")"
  grep -q 'backup.sh' <<< "$script" && fail "--no-backup должен убирать бэкап из скрипта"
  return 0
}

test_remote_script_refuses_a_dirty_checkout() {
  local script
  script="$(release_remote_script abc1234 1)"
  grep -q 'status --porcelain' <<< "$script" ||
    fail "скрипт должен отказываться выкатывать поверх правок, сделанных на сервере"
}

# Сборка ушла с сервера целиком: releaseScript больше не должен звать
# "compose build" ни для одного сервиса. Причина инцидента, ради которого
# всё переписано, — компиляция Go/Vite на боевом (2 ядра, 3.8 ГБ, без swap)
# однажды съела всю память и уложила машину на 45 минут.
test_remote_script_has_no_build_at_all() {
  local script
  script="$(release_remote_script abc1234 1)"
  grep -q 'compose build' <<< "$script" &&
    fail "в удалённом скрипте не должно быть ни одной сборки:
$script"
  return 0
}

# Средство, которым compose явно запрещено лезть за пересборкой, даже если
# сочтёт образ устаревшим: --no-build превращает отсутствие/несовпадение
# образа в явный отказ вместо тихой компиляции на месте.
test_remote_script_up_forbids_rebuild() {
  local script
  script="$(release_remote_script abc1234 1)"
  grep -qE 'compose up -d --no-build' <<< "$script" ||
    fail "подъём стека должен идти с --no-build:
$script"
}

# Ни один сервис не должен выпасть из списка при переносе сборки в другое
# место — раньше именно так пропадал migrate.
test_remote_script_still_starts_every_service() {
  local script
  script="$(release_remote_script abc1234 1)"
  grep -qE -- '--no-build postgres seaweedfs migrate backend frontend caddy$' <<< "$script" ||
    fail "подъём стека должен поднимать все сервисы одной командой:
$script"
}

# Сверка версии обязана идти изнутри сети стека: порт бэкенда наружу не
# публикуется, а в образе бэкенда нет ни curl, ни wget. Проверка через
# 127.0.0.1 или docker exec ... wget не сработает на боевом никогда.
test_version_is_checked_from_inside_the_stack_network() {
  local cmd
  cmd="$(release_verify_cmd)"
  grep -q -- '--network proofreader_default' <<< "$cmd" ||
    fail "запрос версии должен идти из сети стека одноразовым контейнером"
  grep -q 'http://backend:8080/api/version' <<< "$cmd" ||
    fail "адрес должен быть внутренним именем сервиса, а не 127.0.0.1"
  grep -qE 'wget|127\.0\.0\.1' <<< "$cmd" &&
    fail "ни wget, ни 127.0.0.1 на боевом не работают"
  return 0
}

# --ref последним токеном без значения раньше уносил скрипт "shift 2" без
# единого слова: set -u плюс shift за границей — молчаливая смерть.
test_ref_without_a_value_is_rejected() {
  local rc=0
  ( release_parse_args root@203.0.113.4 --ref ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "--ref без значения должен отвергаться"
}

test_ref_with_a_value_is_parsed() {
  release_parse_args root@203.0.113.4 --ref main
  assert_eq "ref" "main" "$REF"
}

# release_build_script — локальная сборка. backend и migrate обязаны
# получиться ОДНИМ вызовом docker build с двумя тегами: тот же Dockerfile,
# те же build-args (docker-compose.yml, после c87aa05d) — значит, побайтно
# одинаковый образ, и гонять его по каналу дважды незачем.
test_build_script_builds_backend_and_migrate_as_one_image() {
  local script backend_line
  script="$(release_build_script abc1234 abc1234)"
  backend_line="$(grep -E 'docker build .*docker/backend/Dockerfile' <<< "$script")"
  [[ -n "$backend_line" ]] || fail "нет сборки docker/backend/Dockerfile:
$script"
  grep -q -- '-t proofreader-backend:latest' <<< "$backend_line" ||
    fail "не тегируется как backend: $backend_line"
  grep -q -- '-t proofreader-migrate:latest' <<< "$backend_line" ||
    fail "не тегируется как migrate — иначе на сервер уедут два одинаковых образа: $backend_line"
}

# COMMIT попадает в бинарник (-ldflags) и в /api/version — без него сверка
# версии после выкатки не сойдётся никогда.
test_build_script_passes_the_commit_into_the_backend_build() {
  local script
  script="$(release_build_script abc1234 abc1234)"
  grep -qE 'docker build .*--build-arg COMMIT=abc1234.*docker/backend/Dockerfile' <<< "$script" ||
    fail "сборка backend/migrate должна получать --build-arg COMMIT=<sha>:
$script"
}

# Явная платформа — гарантия того, что локальная машина и сервер не разъедутся
# по архитектуре молча (сервер всегда x86_64, локальная машина не обязана).
test_build_script_pins_amd64_for_both_builds() {
  local script count
  script="$(release_build_script abc1234 abc1234)"
  count="$(grep -cE 'docker build .*--platform linux/amd64' <<< "$script")"
  (( count == 3 )) ||
    fail "все три сборки (backend/migrate, frontend, mc) должны идти под --platform linux/amd64, нашлось $count:
$script"
}

# frontend — отдельный контекст (Node/Vite, ./frontend/Dockerfile), от
# backend/migrate его отличает, помимо прочего, отсутствие COMMIT в args.
test_build_script_builds_frontend_separately() {
  local script frontend_line
  script="$(release_build_script abc1234 abc1234)"
  frontend_line="$(grep -E 'docker build .*-t proofreader-frontend:latest' <<< "$script")"
  [[ -n "$frontend_line" ]] || fail "нет отдельной сборки frontend:
$script"
  grep -q ':frontend' <<< "$frontend_line" ||
    fail "frontend должен собираться из своего контекста (поддерево ref): $frontend_line"
}

# Ровно три вызова docker build на четыре образа — иначе backend и migrate
# всё-таки собрались бы порознь, и требование «один образ, два тега» тихо
# перестало бы выполняться.
test_build_script_has_exactly_three_build_invocations() {
  local script count
  script="$(release_build_script abc1234 abc1234)"
  count="$(grep -cE 'docker build' <<< "$script")"
  (( count == 3 )) || fail "ожидались три вызова docker build (backend+migrate, frontend, mc), получили $count:
$script"
}

# Клиент MinIO собирается локально и едет на боевой готовым: minio/mc больше
# не публикуется, а без образа на сервере падают backup-prod.sh и restore*.sh.
# Dockerfile берётся из выкатываемого ref, а не из рабочего дерева — тем же
# правилом, что контекст остальных сборок.
test_build_script_builds_the_mc_image_from_the_ref() {
  local script mc_line
  script="$(release_build_script abc1234 abc1234)"
  mc_line="$(grep -E "docker build .*-t $MC_IMAGE" <<< "$script")"
  [[ -n "$mc_line" ]] || fail "нет сборки образа mc ($MC_IMAGE):
$script"
  grep -q 'git show abc1234:docker/mc/Dockerfile |' <<< "$mc_line" ||
    fail "Dockerfile mc должен браться из ref, а не из рабочего дерева: $mc_line"
}

test_check_arch_accepts_a_matching_architecture() {
  release_check_arch x86_64 x86_64 || fail "совпадающая архитектура должна приниматься"
}

test_check_arch_normalizes_amd64_aliases() {
  release_check_arch amd64 x86_64 || fail "amd64 и x86_64 — одна архитектура"
}

test_check_arch_normalizes_arm64_aliases() {
  release_check_arch arm64 aarch64 || fail "arm64 и aarch64 — одна архитектура"
}

# Ровно то, ради чего затевалась проверка: разошедшаяся архитектура — явный
# отказ до передачи, а не exec-format-error после неё.
test_check_arch_rejects_a_mismatch() {
  local rc=0
  ( release_check_arch x86_64 aarch64 ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "разные архитектуры должны отвергаться"
}

# Один поток: docker save | gzip | ssh ... | gunzip | docker load — ни
# локального, ни серверного временного файла с образами.
test_transfer_cmd_streams_all_four_tags() {
  local cmd tag
  cmd="$(release_transfer_cmd root@203.0.113.4)"
  for tag in proofreader-backend:latest proofreader-migrate:latest proofreader-frontend:latest "$MC_IMAGE"; do
    grep -q "$tag" <<< "$cmd" || fail "в потоке нет $tag: $cmd"
  done
  grep -q '^docker save' <<< "$cmd" || fail "поток должен начинаться с docker save: $cmd"
  grep -q 'gzip' <<< "$cmd" || fail "поток должен сжиматься: $cmd"
  grep -q 'gunzip | docker load' <<< "$cmd" || fail "на сервере поток должен распаковаться и загрузиться: $cmd"
}

test_transfer_cmd_uses_no_temporary_file() {
  local cmd
  cmd="$(release_transfer_cmd root@203.0.113.4)"
  grep -qE -- '-o [^ ]+\.tar' <<< "$cmd" && fail "docker save не должен писать архив в файл: $cmd"
  grep -q 'mktemp' <<< "$cmd" && fail "поток образов не должен заводить временный файл: $cmd"
  return 0
}

test_transfer_cmd_targets_the_given_host() {
  local cmd
  cmd="$(release_transfer_cmd root@198.51.100.9)"
  grep -q 'ssh root@198.51.100.9' <<< "$cmd" || fail "поток должен идти именно на переданную цель: $cmd"
}

# Схема считается по дереву того коммита, который едет, а не по рабочему
# каталогу: уезжает-то закоммиченное.
test_local_schema_version_is_the_last_migration_of_the_ref() {
  local got
  got="$(release_local_schema_version HEAD)"
  [[ "$got" =~ ^[0-9]+$ ]] || fail "версия схемы должна быть числом, получено '$got'"
  (( got >= 8 )) || fail "версия схемы подозрительно мала: $got"
}

test_version_check_accepts_a_matching_answer() {
  release_check_version \
    '{"schema_version":10,"schema_dirty":false,"commit":"abc1234"}' abc1234 10 ||
    fail "совпавший ответ должен приниматься"
}

test_version_check_rejects_another_commit() {
  local rc=0
  ( release_check_version \
      '{"schema_version":10,"schema_dirty":false,"commit":"deadbee"}' abc1234 10 \
  ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "чужой коммит должен отвергаться"
}

# Ровно то, что пропускала прежняя сверка: код новый, схема старая.
test_version_check_rejects_an_older_schema() {
  local rc=0
  ( release_check_version \
      '{"schema_version":8,"schema_dirty":false,"commit":"abc1234"}' abc1234 10 \
  ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "отставшая схема должна отвергаться — образ migrate не пересобрался"
}

# schema_dirty отдаётся хендлером и обязан читаться: выкатывать поверх
# оборвавшейся миграции нельзя.
test_version_check_rejects_a_dirty_schema() {
  local rc=0
  ( release_check_version \
      '{"schema_version":10,"schema_dirty":true,"commit":"abc1234"}' abc1234 10 \
  ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "грязная схема должна отвергаться"
}

# 10 не должно совпасть с 1: без границы grep нашёл бы префикс.
test_version_check_does_not_match_a_schema_prefix() {
  local rc=0
  ( release_check_version \
      '{"schema_version":10,"schema_dirty":false,"commit":"abc1234"}' abc1234 1 \
  ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "версия 10 не должна сходить за версию 1"
}

# tmp в main() был local, а ловушка EXIT срабатывает уже после возврата из
# main — под set -u это "unbound variable" на каждом успешном прогоне и
# несработавший rm -rf. Функциональные тесты этого не ловят: они источают
# release.sh, а не запускают его как процесс, поэтому main вообще не
# выполняется. Гоняем реальный процесс с --dry-run (сеть и сервер не нужны) в
# изолированном TMPDIR — так видно и код возврата, и текст ошибки, и то, что
# каталог бандла не остался на диске.
test_dry_run_process_exits_cleanly_and_cleans_up_tmp() {
  local out rc tmpdir
  tmpdir="$(mktemp -d)"
  out="$(TMPDIR="$tmpdir" "$TESTS_DIR/../release.sh" root@203.0.113.4 --dry-run 2>&1)" && rc=0 || rc=$?
  (( rc == 0 )) || fail "release.sh --dry-run должен завершаться нулём, получили $rc:
$out"
  grep -qi 'unbound variable' <<< "$out" &&
    fail "вывод содержит unbound variable — ловушка EXIT смотрит на переменную, которой уже нет:
$out"
  [[ -z "$(ls -A "$tmpdir")" ]] ||
    fail "временный каталог бандла не удалён: $(ls -A "$tmpdir")"
  rm -rf "$tmpdir"
}

# --dry-run обязан не собирать образы и не трогать сеть вовсе: ни ssh, ни scp,
# ни "docker build/save/load/tag". Единственный allowed вызов docker —
# "image inspect" под оценку объёма (release_transfer_estimate), и то
# read-only. Поддельные бинарники в начале PATH проваливают любой другой
# вызов и пишут, что именно их дёрнули — тест ловит не только «ушло на
# сервер», но и «начали собирать образ» ещё до правки, которая разнесла
# сборку и передачу по разным шагам.
test_dry_run_never_touches_docker_build_save_or_the_network() {
  local fakebin log out rc tmpdir
  fakebin="$TMPROOT/fakebin"; mkdir -p "$fakebin"
  log="$TMPROOT/calls.log"; : > "$log"

  cat > "$fakebin/ssh" <<EOF
#!/usr/bin/env bash
echo "ssh \$*" >> "$log"
exit 1
EOF
  cat > "$fakebin/scp" <<EOF
#!/usr/bin/env bash
echo "scp \$*" >> "$log"
exit 1
EOF
  # "image inspect" — единственная разрешённая подкоманда (оценка объёма);
  # изображаем «образов ещё нет», чтобы release_transfer_estimate ушла по
  # честной ветке "оценка недоступна", не разбирая настоящий вывод docker.
  cat > "$fakebin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$log"
if [[ "\$1 \$2" == "image inspect" ]]; then exit 1; fi
exit 1
EOF
  chmod +x "$fakebin/ssh" "$fakebin/scp" "$fakebin/docker"

  tmpdir="$(mktemp -d)"
  out="$(PATH="$fakebin:$PATH" TMPDIR="$tmpdir" "$TESTS_DIR/../release.sh" root@203.0.113.4 --dry-run 2>&1)" && rc=0 || rc=$?
  rm -rf "$tmpdir"
  (( rc == 0 )) || fail "release.sh --dry-run должен завершаться нулём даже с поддельными docker/ssh/scp, получили $rc:
$out"

  if [[ -s "$log" ]]; then
    grep -qv '^docker image inspect' "$log" &&
      fail "--dry-run вызвал что-то, кроме docker image inspect:
$(cat "$log")"
  fi
  return 0
}

# Сердцевина защиты от боевого случая 27.08.2026: неотслеживаемая миграция из
# чужой ветки уехала в образ migrate через контекст docker build и применилась
# на боевом. Контекстом обязана быть выгрузка ref, где неотслеживаемого нет по
# построению. Тест закрепляет само свойство git archive, на которое мы теперь
# опираемся, — без него правка держится на вере.
test_git_archive_leaves_untracked_files_out() {
  local origin listing
  origin="$(mktemp -d)"
  git -C "$origin" init --quiet
  git -C "$origin" config user.email t@t; git -C "$origin" config user.name t
  mkdir -p "$origin/internal/database/migrations"
  echo 'CREATE TABLE tracked (id int);' > "$origin/internal/database/migrations/000001_ok.up.sql"
  git -C "$origin" add -A; git -C "$origin" commit --quiet -m first
  # ровно та ловушка: файл лежит в каталоге, но git его не знает
  echo 'CREATE TABLE untracked (id int);' > "$origin/internal/database/migrations/000099_left_over.up.sql"

  listing="$(git -C "$origin" archive --format=tar HEAD | tar -t)"
  grep -q '000001_ok.up.sql' <<< "$listing" ||
    fail "отслеживаемая миграция не попала в контекст:
$listing"
  grep -q '000099_left_over' <<< "$listing" &&
    fail "НЕотслеживаемая миграция попала в контекст — ровно так 000014 уехала на боевой:
$listing"

  rm -rf -- "$origin"
  return 0
}

# Контекст обязан приходить из git archive, а не из рабочего каталога. Точки и
# ./frontend в конце docker build быть не должно: именно они и были причиной.
test_build_script_takes_context_from_git_archive_not_the_work_tree() {
  local script line
  script="$(release_build_script abc1234 abc1234)"
  while IFS= read -r line; do
    grep -qE ' -$' <<< "$line" ||
      fail "контекст docker build должен приходить из stdin (аргумент \"-\"):
$line"
    # mc собирается без контекста — в stdin идёт один Dockerfile, и он тоже
    # берётся из ref (git show), а не из рабочего дерева.
    grep -qE '^git (archive --format=tar abc1234|show abc1234:)' <<< "$line" ||
      fail "перед docker build должен стоять git archive (или git show) нужного ref:
$line"
  done < <(grep -E 'docker build' <<< "$script")

  grep -qE 'docker build[^|]*[[:space:]]\.$' <<< "$script" &&
    fail "рабочий каталог как контекст — та самая причина случая 27.08.2026:
$script"
  grep -qF './frontend' <<< "$script" &&
    fail "фронт должен собираться из поддерева ref, а не из ./frontend в дереве:
$script"
  return 0
}

# Фронт берёт поддерево ref: frontend/Dockerfile пишет COPY относительно
# своего каталога. Отдай ему корень репозитория — сборка развалится на COPY
# package*.json.
test_build_script_feeds_frontend_the_subtree() {
  local script
  script="$(release_build_script abc1234 abc1234)"
  grep -qE 'git archive --format=tar abc1234:frontend \| docker build .* -t proofreader-frontend:latest' <<< "$script" ||
    fail "фронт должен собираться из <ref>:frontend:
$script"
  return 0
}

# run_remote <содержимое .env сервера> <бэкап?> — исполнить удалённый скрипт
# над подставным сервером: /opt/proofreader подменён на $TMPROOT, git и docker
# подставные, их вызовы — в $TMPROOT/calls. Код возврата — скрипта.
run_remote() {
  local env="$1" do_backup="$2" script
  mkdir -p "$TMPROOT/app" "$TMPROOT/bin"
  : > "$TMPROOT/calls"
  printf '%s\n' "$env" > "$TMPROOT/app/.env"
  cat > "$TMPROOT/bin/git" <<FAKE
#!/usr/bin/env bash
printf 'git %s\n' "\$*" >> "$TMPROOT/calls"
FAKE
  cat > "$TMPROOT/bin/docker" <<FAKE
#!/usr/bin/env bash
printf 'docker %s\n' "\$*" >> "$TMPROOT/calls"
FAKE
  chmod +x "$TMPROOT/bin/git" "$TMPROOT/bin/docker"
  script="$(release_remote_script abc1234 "$do_backup")"
  script="${script//\/opt\/proofreader/$TMPROOT}"
  PATH="$TMPROOT/bin:$PATH" bash -c "$script" >/dev/null 2>"$TMPROOT/stderr"
}

up_line() { grep 'compose.* up -d' "$TMPROOT/calls" || true; }

test_remote_script_starts_seaweedfs_for_local_storage() {
  # Шаг 0 runbook-а мог быть забыт: COMPOSE_PROFILES в .env нет, а seaweedfs
  # обязан подняться всё равно — явным именем.
  run_remote 'S3_ENDPOINT=http://seaweedfs:8333' "" || { fail "релиз упал: $(cat "$TMPROOT/stderr")"; return 0; }
  [[ "$(up_line)" == *" seaweedfs "* ]] || fail "местное хранилище — seaweedfs обязан быть в compose up: $(up_line)"
}

test_remote_script_skips_seaweedfs_for_external_storage() {
  run_remote 'S3_ENDPOINT=https://s3.example.org' "" || { fail "релиз упал: $(cat "$TMPROOT/stderr")"; return 0; }
  [[ -n "$(up_line)" ]] || { fail "compose up не вызван"; return 0; }
  if [[ "$(up_line)" == *seaweedfs* ]]; then
    fail "внешнее хранилище — seaweedfs в compose up быть не должно: $(up_line)"
  fi
  [[ "$(up_line)" == *" migrate backend frontend caddy"* ]] || fail "остальной стек обязан подняться: $(up_line)"
}

test_remote_script_refuses_a_backup_with_external_storage_before_checkout() {
  local rc=0
  run_remote 'S3_ENDPOINT=https://s3.example.org' 1 || rc=$?
  (( rc != 0 )) || fail "--with-backup при внешнем хранилище обязан отказать"
  grep -q 'checkout' "$TMPROOT/calls" 2>/dev/null &&
    fail "отказ обязан прийти ДО переключения кода: $(cat "$TMPROOT/calls")"
  grep -q 'внешн' "$TMPROOT/stderr" || fail "в отказе нет причины: $(cat "$TMPROOT/stderr")"
  return 0
}

run_tests
