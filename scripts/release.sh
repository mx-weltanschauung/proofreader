#!/usr/bin/env bash
# Выкатка кода и схемы на боевой сервер. Данные не трогает.
#
#   ./scripts/release.sh root@203.0.113.4 [--ref HEAD] [--with-backup] [--dry-run]
#
# Снимок данных по умолчанию НЕ снимается: на боевом backup.sh зеркалит сканы
# целиком (~22 ГБ), а диск второй копии не вмещает. Нужен снимок — попроси
# явно: --with-backup. Прежний --no-backup принимается и означает умолчание.
#
# Код едет git-бандлом, как при разворачивании: git-сервера у проекта нет.
# Образы едут отдельным потоком (docker save | gzip | ssh | docker load) —
# сервер их только грузит, не собирает. Причина: сборка Go и Vite на боевом
# (2 ядра, 3.8 ГБ, без swap) однажды съела всю память и уложила машину на
# 45 минут (ping шёл, sshd — нет), помогла только жёсткая перезагрузка через
# панель хостера. Восстановления здесь не бывает — за данными ходят
# backup.sh/restore.sh.
#
# Источается тестами — тогда main не запускается.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

REMOTE_ROOT=/opt/proofreader
# Явная архитектура сборки — не "то, что покажет локальный `docker build`",
# а фиксированное значение: сервер x86_64, и если локальная машина когда-нибудь
# станет другой архитектуры, образ обязан всё равно собраться под amd64, а не
# молча получиться нативным и не запуститься на сервере.
PLATFORM=linux/amd64
# Backend и migrate — один и тот же Dockerfile с одинаковыми build-args
# (docker-compose.yml, после c87aa05d); проверено `docker compose config
# --images` на этом дереве. Поэтому это один `docker build` с двумя тегами,
# а не два образа. Имена — ровно то, что называет `docker compose config
# --images` при имени проекта "proofreader" (см. `name:` в docker-compose.yml):
# без явного `image:` в сервисе Compose сам тегирует собранное как
# "<проект>-<сервис>:latest", и этому же имени должен отвечать `docker load`
# на сервере, иначе `compose up --no-build` его не найдёт.
IMAGE_BACKEND=proofreader-backend:latest
IMAGE_MIGRATE=proofreader-migrate:latest
IMAGE_FRONTEND=proofreader-frontend:latest

TARGET=""; REF="HEAD"; DO_BACKUP=""; DRY_RUN=""
# Глобальная, а не local в main: ловушка EXIT срабатывает уже после возврата
# из main, и local-переменная к этому моменту не существует — под set -u это
# "unbound variable" на каждом успешном прогоне, а rm -rf не выполняется и
# временный каталог с бандлом остаётся на диске. Пустая строка по умолчанию
# вместе с проверкой в ловушке — чтобы rm -rf не мог выстрелить по пустому
# или ещё не назначенному пути.
tmp=""

usage() { sed -n '2,13p' "${BASH_SOURCE[0]}" >&2; exit "${1:-1}"; }

release_parse_args() {
  TARGET=""; REF="HEAD"; DO_BACKUP=""; DRY_RUN=""
  while (( $# )); do
    case "$1" in
      --ref)       [[ $# -ge 2 ]] || die "--ref без значения"
                   REF="$2"; shift 2 ;;
      --with-backup) DO_BACKUP=1; shift ;;
      # Умолчание, а не выключатель: флаг оставлен принимаемым, чтобы прежние
      # заметки и привычка рук не роняли выкатку на безобидном аргументе.
      --no-backup) DO_BACKUP=""; shift ;;
      --dry-run)   DRY_RUN=1; shift ;;
      -h | --help) usage 0 ;;
      -*) die "неизвестный флаг: $1" ;;
      *)  [[ -z "$TARGET" ]] || die "цель уже задана ($TARGET), лишний аргумент: $1"
          TARGET="$1"; shift ;;
    esac
  done
  [[ -n "$TARGET" ]] || die "нужна цель вида root@адрес"
  [[ "$TARGET" == *@* ]] || die "цель должна быть вида пользователь@адрес (нужен root): получено '$TARGET'"
}

# release_verify_cmd — команда, которой выкатка спрашивает у боевого его версию.
# Ходить приходится изнутри сети стека одноразовым контейнером: порт бэкенда
# наружу не публикуется (ports: !reset [] в docker-compose.prod.yml), а в самом
# образе бэкенда нет ни curl, ни wget — debian:bookworm-slim плюс три пакета.
# Тот же приём, что у curl_in в bootstrap.sh.
release_verify_cmd() {
  printf '%s\n' 'docker run --rm --network proofreader_default curlimages/curl -sS -f http://backend:8080/api/version'
}

# release_remote_script <sha> <бэкап?> — текст скрипта, который выполнится на
# сервере. Отдельной функцией, чтобы его можно было проверить тестами, не
# заходя на сервер.
release_remote_script() {
  local sha="$1" do_backup="$2"
  cat <<REMOTE
set -Eeuo pipefail
APP=$REMOTE_ROOT/app
BUNDLE=$REMOTE_ROOT/update.bundle

compose() {
  docker compose --project-directory "\$APP" \\
    -f "\$APP/docker-compose.yml" -f "\$APP/docker-compose.prod.yml" "\$@"
}

# Режим хранилища — свойство сервера, а не релиза: решает S3_ENDPOINT его
# .env. Функция — копия из scripts/lib.sh (declare -f), чтобы правило было
# одно (спека 2026-10-02-external-s3-storage).
$(declare -f endpoint_is_external)
external=""
if endpoint_is_external "\$(grep -oP '^S3_ENDPOINT=\K.*' "\$APP/.env" || true)"; then
  external=1
fi
$( [[ -n "$do_backup" ]] && printf '%s\n' \
  '# backup.sh зеркалит местный SeaweedFS; при внешнем хранилище его нет.' \
  'if [ -n "$external" ]; then' \
  '  echo "ERROR: --with-backup при внешнем хранилище невозможен: backup.sh работает только с местным SeaweedFS" >&2' \
  '  exit 1' \
  'fi' )

# Правки, сделанные прямо на сервере, — не то, что можно молча снести
# переключением коммита: сначала пусть человек решит, что с ними делать.
dirty="\$(git -C "\$APP" status --porcelain)"
if [ -n "\$dirty" ]; then
  echo "ERROR: в \$APP есть несохранённые правки:" >&2
  echo "\$dirty" >&2
  exit 1
fi

git -C "\$APP" remote set-url origin "\$BUNDLE"
git -C "\$APP" fetch --quiet origin
git -C "\$APP" checkout --detach $sha
echo "код на \$(git -C "\$APP" rev-parse --short HEAD)"
$( [[ -n "$do_backup" ]] && printf '%s\n' \
  '# Снимок до миграции: единственное, что стоит между опечаткой в .down.sql и корпусом.' \
  '( cd "$APP" && ./scripts/backup.sh )' )
# Образы сюда доставил "docker load" ещё до этого шага (см. release.sh) —
# компиляции здесь больше нет ни одной. --no-build запрещает compose лезть
# за пересборкой, даже если решит, что образ устарел: одна попытка
# скомпилировать Go/Vite на боевом (2 ядра, 3.8 ГБ, без swap) уже укладывала
# машину на 45 минут. Если образа нет или тег не совпал — явный отказ вместо
# тихой компиляции.
# seaweedfs — явным именем при местном хранилище: явно названный сервис
# compose поднимает и под профилем, даже если COMPOSE_PROFILES в .env забыт.
if [ -n "\$external" ]; then
  compose up -d --no-build postgres migrate backend frontend caddy
else
  compose up -d --no-build postgres seaweedfs migrate backend frontend caddy
fi
REMOTE
}

# Контекст сборки — `git archive <ref>` пайпом в `docker build -`, а не
# рабочий каталог.
#
# Появилось после боевого случая 27.08.2026. Сборка шла с контекстом
# «рабочий каталог» (`docker build ... .`), а Dockerfile делает
# `COPY internal/database/migrations ...`. Docker про отслеживаемость файлов
# не знает, .dockerignore про неё сказать не может — и неотслеживаемая
# миграция 000014 из чужой ветки, положенная в каталог рядом, уехала в образ
# migrate и применилась на боевом. Схема ушла на 14 при коде, ждущем 13;
# спасло только то, что миграция была аддитивной и пустой.
#
# Закрывается весь класс, а не этот случай: следующим уехал бы чей-нибудь
# недописанный .env или тестовая фикстура. Обещание скилла «уедет только
# закоммиченное» становится правдой и для образов, а не только для кода на
# сервере.
#
# Почему пайпом, а не распаковкой во временный каталог: первая версия так и
# делала, и на живой сборке выяснилось, что демон Docker не видит путей в
# /tmp (`unable to prepare context: path "/tmp/tmp.XXXX" not found`) — то
# есть выкатка сломалась бы целиком. Контекст из stdin пути не имеет вовсе, а
# `-f` при этом указывает путь ВНУТРИ архива.
#
# Фронт берёт поддерево: `git archive <ref>:frontend` кладёт содержимое
# frontend/ в корень архива, потому что frontend/Dockerfile пишет COPY
# относительно своего каталога, а не корня репозитория.

# release_build_script <sha> <ref> — текст локальной сборки образов.
# Отдельная функция ровно по той же причине, что и release_remote_script:
# проверяется грепом, без реального docker build.
release_build_script() {
  local sha="$1" ref="$2"
  cat <<BUILD
set -Eeuo pipefail
cd "$REPO_ROOT"
# backend и migrate — один и тот же Dockerfile с одинаковыми build-args
# (docker-compose.yml, после c87aa05d): собираем один раз и тегируем дважды —
# по каналу поедет один образ, а не два одинаковых по 150+ МБ.
git archive --format=tar $ref | docker build --platform $PLATFORM --build-arg COMMIT=$sha -f docker/backend/Dockerfile -t $IMAGE_BACKEND -t $IMAGE_MIGRATE -
# Свой контекст (Node/Vite), своего COMMIT в args не принимает — коммит в
# /api/version отдаёт только backend. Поддерево ref, чтобы COPY в
# frontend/Dockerfile считался от frontend/, а не от корня репозитория.
git archive --format=tar $ref:frontend | docker build --platform $PLATFORM -t $IMAGE_FRONTEND -f Dockerfile -
# Клиент MinIO для backup.sh/backup-prod.sh/restore*.sh на боевом (mc_run в
# lib.sh): minio/mc больше не публикуется, а собирать на сервере нельзя по
# той же причине, что и остальное. Контекста у него нет — исходник mc
# приходит из Go module proxy, — поэтому в stdin идёт один Dockerfile из ref.
# Слои закэшированы, со второй выкатки сборка мгновенная.
git show $ref:docker/mc/Dockerfile | docker build --platform $PLATFORM -t $MC_IMAGE -
BUILD
}

# release_normalize_arch <uname -m> — синонимы одной архитектуры к одному имени,
# чтобы x86_64 с локальной машины не разошёлся с amd64 из другого источника.
release_normalize_arch() {
  case "$1" in
    x86_64 | amd64) printf 'amd64\n' ;;
    aarch64 | arm64) printf 'arm64\n' ;;
    *) printf '%s\n' "$1" ;;
  esac
}

# release_check_arch <локальная uname -m> <серверная uname -m> — отказ до
# передачи, а не exec-format-error после неё: образы собраны под linux/amd64
# явно (см. PLATFORM), и на другой архитектуре просто не запустятся.
release_check_arch() {
  local local_arch="$1" remote_arch="$2"
  [[ "$(release_normalize_arch "$local_arch")" == "$(release_normalize_arch "$remote_arch")" ]] ||
    die "архитектура сервера ($remote_arch) не совпадает с локальной ($local_arch) — образы собраны под $PLATFORM и на другой архитектуре не запустятся"
}

# release_transfer_cmd <цель> — команда одним потоком: docker save | gzip |
# ssh | gunzip | docker load. Ни локального, ни серверного временного файла:
# на сервере 59 ГБ диска, которые он не должен тратить ещё и на архив, а на
# локальной машине временный файл архива тоже не нужен ради одной передачи.
# pv, если он есть, — только индикатор прогресса поверх того же потока, без
# доп. буферизации; если pv нет — поток идёт как есть, без прогресса.
release_transfer_cmd() {
  local target="$1"
  local imgs="$IMAGE_BACKEND $IMAGE_MIGRATE $IMAGE_FRONTEND $MC_IMAGE"
  if command -v pv >/dev/null 2>&1; then
    printf 'docker save %s | gzip | pv -N "передача образов" | ssh %s '\''gunzip | docker load'\''\n' \
      "$imgs" "$target"
  else
    printf 'docker save %s | gzip | ssh %s '\''gunzip | docker load'\''\n' "$imgs" "$target"
  fi
}

# release_format_transfer_estimate <размер backend> <размер frontend> —
# человекочитаемая оценка потока. backend и migrate не складываются: это один
# и тот же образ (см. IMAGE_BACKEND/IMAGE_MIGRATE), их общие слои "docker save"
# запишет один раз.
release_format_transfer_estimate() {
  local backend_size="$1" frontend_size="$2" total
  total=$(( backend_size + frontend_size ))
  printf 'backend/migrate (общий образ) ~%s + frontend ~%s ≈ %s до сжатия (gzip уменьшит поток; поверх — общие базовые слои debian/nginx, которые сервер уже видел на прошлой выкатке)\n' \
    "$(numfmt --to=iec "$backend_size")" "$(numfmt --to=iec "$frontend_size")" "$(numfmt --to=iec "$total")"
}

# release_transfer_estimate — оценка объёма для --dry-run, только если она
# дешёвая: по образам, уже собранным локально раньше (docker build здесь не
# запускается). Если их нет — честно говорим, что оценивать пока нечем, а не
# гадаем.
release_transfer_estimate() {
  local backend_size frontend_size
  backend_size="$(docker image inspect -f '{{.Size}}' "$IMAGE_BACKEND" 2>/dev/null)" || true
  frontend_size="$(docker image inspect -f '{{.Size}}' "$IMAGE_FRONTEND" 2>/dev/null)" || true
  if [[ -z "$backend_size" || -z "$frontend_size" ]]; then
    printf 'оценка недоступна: локальных образов %s/%s ещё нет (появятся после первой сборки)\n' \
      "$IMAGE_BACKEND" "$IMAGE_FRONTEND"
    return 0
  fi
  # Образы могли остаться от прежней локальной сборки (другой коммит) — это
  # прикидка объёма, а не гарантия; так и подписано в самом выводе функции.
  release_format_transfer_estimate "$backend_size" "$frontend_size"
}

# release_local_schema_version <ref> — номер последней миграции в дереве <ref>.
# Именно он должен оказаться на боевом после выкатки: сверять код, не сверяя
# схему, значит проглядеть непересобранный образ migrate, который молча
# прогоняет старый каталог миграций и выходит с нулём.
release_local_schema_version() {
  git -C "$REPO_ROOT" ls-tree --name-only "$1" internal/database/migrations/ |
    sed -nE 's#.*/0*([0-9]+)_.*\.up\.sql$#\1#p' | sort -n | tail -1
}

# release_check_version <тело ответа> <sha> <версия схемы> — сверка боевого.
# Три вещи, а не одна: коммит доказывает, что поднялся новый бэкенд; версия
# схемы — что миграции доехали; чистота — что последняя не оборвалась на
# середине, оставив номер стоять, а таблицы недоделанными.
release_check_version() {
  local got="$1" sha="$2" schema="$3"
  grep -q "\"commit\":\"$sha\"" <<< "$got" ||
    die "на боевом другой коммит: $got (ждали $sha)"
  grep -qE "\"schema_version\":$schema[,}]" <<< "$got" ||
    die "на боевом другая версия схемы: $got (ждали $schema) — похоже, привезённый образ migrate устарел или не тот"
  grep -q '"schema_dirty":false' <<< "$got" ||
    die "на боевом грязная схема: $got — миграция оборвалась, разбирайтесь на сервере"
}

main() {
  release_parse_args "$@"

  local sha
  sha="$(git -C "$REPO_ROOT" rev-parse --short "$REF")" || die "неизвестный ref: $REF"
  [[ -z "$(git -C "$REPO_ROOT" status --porcelain)" ]] ||
    log "ВНИМАНИЕ: в рабочем дереве есть незакоммиченные правки — на сервер уедет только закоммиченное ($sha)"

  # Про снимок говорим словами, а не отсутствием строки в плане: человек
  # должен видеть в логе, чем именно застрахована эта выкатка.
  if [[ -n "$DO_BACKUP" ]]; then
    log "снимок данных на сервере: снимаю до миграции (backup.sh, зеркалит и сканы — это долго)"
  else
    log "снимок данных на сервере: не снимаю (умолчание; нужен — --with-backup)"
  fi

  tmp="$(mktemp -d)"; trap '[[ -n "$tmp" && -d "$tmp" ]] && rm -rf -- "$tmp"' EXIT
  log "собираю бандл на $sha"
  git -C "$REPO_ROOT" bundle create "$tmp/update.bundle" --all >/dev/null 2>&1 ||
    die "не собрался git bundle"

  if [[ -n "$DRY_RUN" ]]; then
    log "--dry-run: сервер не потрогаю. План:"
    log "1) сверить архитектуру $TARGET (uname -m) с локальной ($(uname -m))"
    log "2) собрать образы локально ($PLATFORM):"
    release_build_script "$sha" "$sha"
    log "3) бандл на $sha + поток образов на $TARGET:"
    release_transfer_cmd "$TARGET"
    log "оценка объёма потока: $(release_transfer_estimate)"
    log "4) выполнить на сервере:"
    release_remote_script "$sha" "$DO_BACKUP"
    return 0
  fi

  # До того, как поедут сотни мегабайт: разошедшаяся архитектура иначе
  # обнаружится только exec-format-error'ом внутри уже поднятого контейнера.
  log "проверяю архитектуру $TARGET"
  local local_arch remote_arch
  local_arch="$(uname -m)"
  remote_arch="$(ssh "$TARGET" uname -m)" || die "не достучался до $TARGET, чтобы сверить архитектуру"
  release_check_arch "$local_arch" "$remote_arch"

  log "собираю образы локально ($PLATFORM, COMMIT=$sha)"
  bash -c "$(release_build_script "$sha" "$sha")" || die "не собрались образы"

  log "везу бандл на $TARGET"
  scp -q "$tmp/update.bundle" "$TARGET:$REMOTE_ROOT/update.bundle" ||
    die "не удалось скопировать бандл"

  log "везу образы на $TARGET"
  bash -c "$(release_transfer_cmd "$TARGET")" || die "не удалось перекачать образы"

  log "выкатываю"
  release_remote_script "$sha" "$DO_BACKUP" | ssh "$TARGET" bash -s ||
    die "выкатка на сервере не удалась"

  # Проверка не по коду ответа, а по коммиту: собранный, но не поднявшийся
  # контейнер оставил бы работать прежний бэкенд, и выкатка выглядела бы удачной.
  log "сверяю версию"
  local got schema
  schema="$(release_local_schema_version "$sha")"
  [[ -n "$schema" ]] || die "не нашёл миграций в дереве $sha — сверять схему не с чем"
  got="$(ssh "$TARGET" "$(release_verify_cmd)")" ||
    die "боевой не ответил на /api/version"
  release_check_version "$got" "$sha" "$schema"
  log "готово: $got"
}

# Не запускаться при source из тестов.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
