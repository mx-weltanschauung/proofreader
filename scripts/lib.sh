#!/usr/bin/env bash
# Shared helpers for backup/restore/reset. Source this; do not execute.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

load_config() {
  # Приоритет: окружение вызывающего > .env > умолчания. Значения, заданные
  # вызывающим, запоминаются до `source .env` и возвращаются после него —
  # иначе `set -a; source .env` их затрёт.
  local _keep=() _v
  for _v in DB_NAME DB_USER S3_BUCKET S3_AUDIO_BUCKET S3_KEY S3_SECRET S3_ENDPOINT \
            EXT_S3_ENDPOINT EXT_S3_KEY EXT_S3_SECRET PG_CONTAINER SW_CONTAINER NET; do
    if [[ -n "${!_v:-}" ]]; then _keep+=("$_v=${!_v}"); fi
  done

  if [[ -f "$REPO_ROOT/.env" ]]; then
    set -a; # shellcheck disable=SC1091
    source "$REPO_ROOT/.env"; set +a
  fi

  for _v in ${_keep[@]+"${_keep[@]}"}; do export "${_v?}"; done

  : "${DB_NAME:=proofreader}"
  : "${DB_USER:=proofreader}"
  : "${S3_BUCKET:=proofreader}"
  : "${S3_KEY:=${S3_ACCESS_KEY:-proofreader_dev}}"
  : "${S3_SECRET:=${S3_SECRET_KEY:-proofreader_dev_secret}}"
  : "${PG_CONTAINER:=proofreader-postgres}"
  : "${SW_CONTAINER:=proofreader-seaweedfs}"
  : "${NET:=proofreader_default}"
  : "${S3_AUDIO_BUCKET:=proofreader-audio}"
  : "${S3_ENDPOINT:=}"
  # Внешнее хранилище — оно же адрес ext для mc_run, если вызывающий не задал
  # его сам (переливка на боевом задаёт: .env там ещё местный).
  if storage_is_external; then
    : "${EXT_S3_ENDPOINT:=$S3_ENDPOINT}"
    : "${EXT_S3_KEY:=$S3_KEY}"
    : "${EXT_S3_SECRET:=$S3_SECRET}"
  fi
  : "${EXT_S3_ENDPOINT:=}"; : "${EXT_S3_KEY:=}"; : "${EXT_S3_SECRET:=}"
  # Снапшот один и тот же для источника и цели — каталог бэкапов общий.
  BACKUP_ROOT="$REPO_ROOT/backups"
  export DB_NAME DB_USER S3_BUCKET S3_AUDIO_BUCKET S3_KEY S3_SECRET S3_ENDPOINT \
    EXT_S3_ENDPOINT EXT_S3_KEY EXT_S3_SECRET PG_CONTAINER SW_CONTAINER NET BACKUP_ROOT
}

# endpoint_is_external <адрес> — 0, если хранилище не местный SeaweedFS.
# Местный — это compose (seaweedfs), машина разработчика (localhost,
# 127.0.0.1: .env указывает на http://localhost:8333) и одноразовое окружение
# verify-restore.sh. Пустой адрес — местный: так ведут себя все скрипты до
# появления внешнего хранилища. Спека 2026-10-02-external-s3-storage.
endpoint_is_external() {
  local host="${1#*://}"
  host="${host%%[:/]*}"
  [[ -n "$host" ]] || return 1
  case "$host" in
    seaweedfs | localhost | 127.0.0.1 | proofreader-*seaweedfs) return 1 ;;
  esac
  return 0
}

storage_is_external() { endpoint_is_external "${S3_ENDPOINT:-}"; }

# refuse_external_storage <что> — отказ скрипта, который умеет только местный
# SeaweedFS (зеркалит бакет на диск, льёт его обратно, сносит). Первым делом,
# до любого docker: иначе он упал бы на seaweedfs:8333 посреди работы.
refuse_external_storage() {
  storage_is_external || return 0
  die "$1: хранилище внешнее ($S3_ENDPOINT) — скрипт работает только с местным SeaweedFS. Копия сканов при внешнем хранилище — проект Б (docs/superpowers/specs/2026-10-02-external-s3-storage-design.md)"
}

# mc_host_url <адрес> <ключ> <секрет> — адрес для MC_HOST_<имя> с вшитым
# удостоверением: https://s3.example.org -> https://KEY:SECRET@s3.example.org. Ключи
# обязаны быть буквами и цифрами — экранирования здесь нет (тот же довод, что
# у MC_HOST_seaweed ниже).
mc_host_url() {
  local ep="${1%/}"
  printf '%s://%s:%s@%s\n' "${ep%%://*}" "$2" "$3" "${ep#*://}"
}

require_running() {
  local c="$1"
  local state
  state="$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null || echo missing)"
  [[ "$state" == "true" ]] || die "container '$c' is not running. Start the stack with: make docker-up"
}

# Клиент MinIO — свой образ, собранный из исходников (docker/mc/Dockerfile).
# minio/mc больше не публикуется нигде: проект заархивирован, Docker Hub
# отвечает "pull access denied", dl.min.io — 410 Gone. Тег несёт релиз и
# обязан совпадать с MC_RELEASE в Dockerfile (сторожит mc_image_test.sh):
# сменил коммит — смени и тег, иначе машина с прежним образом в кэше молча
# останется на старом mc.
MC_IMAGE=proofreader-mc:RELEASE.2025-08-13T08-35-41Z

# ensure_mc_image — собрать образ mc, если его ещё нет на машине (свежий
# сервер на фазе infra bootstrap.sh, новая локальная машина). На живой
# боевой образ привозит release.sh, собирать его там не придётся. Вывод
# сборки — только в stderr: mc_run зовут внутри $(…), и stdout обязан
# остаться выводом самого mc.
ensure_mc_image() {
  docker image inspect "$MC_IMAGE" >/dev/null 2>&1 && return 0
  log "образа $MC_IMAGE нет — собираю из docker/mc/Dockerfile (около минуты)"
  docker build -t "$MC_IMAGE" - < "$REPO_ROOT/docker/mc/Dockerfile" >&2 ||
    die "не собрался образ $MC_IMAGE (docker/mc/Dockerfile)"
}

# mc_run <mc-args...>   (set MC_MOUNT=<host dir> to bind it at /data)
mc_run() {
  ensure_mc_image
  local mount_args=() ext_args=()
  [[ -n "${MC_MOUNT:-}" ]] && mount_args=(-v "$MC_MOUNT:/data")
  # Внешний S3 — второй адрес в том же контейнере: переливка seaweed -> ext
  # идёт одним mc mirror. Без EXT_S3_ENDPOINT адреса нет вовсе.
  if [[ -n "${EXT_S3_ENDPOINT:-}" ]]; then
    ext_args=(-e "MC_HOST_ext=$(mc_host_url "$EXT_S3_ENDPOINT" "$EXT_S3_KEY" "$EXT_S3_SECRET")")
  fi
  # MC_HOST_seaweed embeds the key/secret directly in a URL (user:pass@host), which is
  # only safe because dev creds are alnum+underscore. If S3_KEY/S3_SECRET ever contain
  # URL-reserved characters (@ : / %), they must be percent-encoded here or this breaks.
  #
  # --user: без него контейнер пишет в примонтированный /data от root, и снапшот
  # достаётся root:root 755. Хозяин каталога тогда не может ни поправить бэкап, ни
  # удалить его: `rm -rf` не войдёт в чужой каталог без права записи, то есть
  # prune_backups падает на первой же чистке. HOME задан потому, что своего
  # /etc/passwd у чужого uid нет, а mc кладёт конфиг в $HOME/.mc.
  docker run --rm --network "$NET" "${mount_args[@]}" \
    --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e "MC_HOST_seaweed=http://$S3_KEY:$S3_SECRET@seaweedfs:8333" \
    "${ext_args[@]}" \
    "$MC_IMAGE" "$@"
}

# pick_listed_page <листинг> — stdin: строки «work|page|номер|ключ превью» в
# нужном порядке; stdout: первая строка, чей ключ лежит в листинге (файл
# строк «ключ размер»). Нужен смоку verify-restore.sh: локальный бакет держит
# только рабочий набор, и полоса «наименьшего тома» почти наверняка выселена.
# listing_has_key <листинг> <ключ> — код 0, если первое поле какой-то строки
# листинга («ключ размер») равно ключу.
listing_has_key() {
  awk -v k="$2" '$1 == k { f = 1 } END { exit !f }' "$1"
}

pick_listed_page() {
  awk -F'|' 'NR == FNR { split($0, a, " "); have[a[1]] = 1; next }
             ($4 in have) { print; exit }' "$1" -
}

# snapshot_meta <snap> <workdir> — путь к каталогу, в котором лежит готовый db.dump.
# Старая раскладка (db.dump прямо в снапшоте) отдаёт сам снапшот; новая (db.dump и
# repo.bundle упакованы в meta.tar) распаковывает архив в <workdir> и отдаёт его.
# Снапшот в обоих случаях только читается. manifest.json лежит снаружи архива
# всегда — его читают грепом, не распаковывая.
snapshot_meta() {
  local snap="$1" work="$2" dir
  if [[ -f "$snap/db.dump" ]]; then
    dir="$snap"
  elif [[ -f "$snap/meta.tar" ]]; then
    [[ -d "$work" ]] || die "snapshot_meta: каталог распаковки не найден: $work"
    tar -xf "$snap/meta.tar" -C "$work" ||
      die "snapshot_meta: не удалось распаковать $snap/meta.tar"
    dir="$work"
  else
    die "snapshot_meta: в снапшоте нет ни db.dump, ни meta.tar: $snap"
  fi
  [[ -s "$dir/db.dump" ]] || die "snapshot_meta: db.dump пуст или отсутствует: $dir/db.dump"
  printf '%s\n' "$dir"
}

# pick_snapshot <root> <latest|имя|путь> — путь к целому снапшоту.
# Общая для yadisk-sync.sh (что заливать на Диск) и deploy.sh (что везти на сервер).
pick_snapshot() {
  local root="$1" want="${2:-latest}" dir="" name
  if [[ "$want" == "latest" ]]; then
    # Метка времени в имени сортируется лексикографически = хронологически,
    # тем же приёмом, что и prune_backups.
    while IFS= read -r name; do
      if [[ -e "$root/$name/.INCOMPLETE" ]]; then continue; fi
      dir="$root/$name"
      break
    done < <(
      find "$root" -mindepth 1 -maxdepth 1 -type d -name 'proofreader-*' -printf '%f\n' \
        2>/dev/null | sort -r
    )
    [[ -n "$dir" ]] || die "pick_snapshot: в $root нет ни одного целого снапшота — сними бэкап: ./scripts/backup.sh"
  else
    want="${want%/}" # хвостовой слэш приезжает из автодополнения оболочки
    if [[ "$want" == */* ]]; then dir="$want"; else dir="$root/$want"; fi
    [[ -d "$dir" ]] || die "pick_snapshot: снапшот не найден: $dir"
    [[ -e "$dir/.INCOMPLETE" ]] &&
      die "pick_snapshot: снапшот битый (.INCOMPLETE), заливать нечего: $dir"
  fi
  printf '%s\n' "$dir"
}

# prune_backups <root> <keep> — оставить <keep> свежих снапшотов в <root>
prune_backups() {
  local root="$1" keep="$2"
  local names=() name i
  [[ "$keep" =~ ^[0-9]+$ ]] ||
    die "prune_backups: лимит должен быть целым неотрицательным числом, получено '$keep' (BACKUP_KEEP)"
  # keep=0 — чистка выключена; ни целые, ни битые снапшоты не трогаем.
  (( keep > 0 )) || return 0
  while IFS= read -r name; do
    # Битый снапшот (упавший бэкап) сносится всегда и слота не занимает.
    if [[ -e "$root/$name/.INCOMPLETE" ]]; then
      log "removing incomplete snapshot $name"
      rm -rf "${root:?}/$name"
    else
      names+=("$name")
    fi
  done < <(
    find "$root" -mindepth 1 -maxdepth 1 -type d -name 'proofreader-*' -printf '%f\n' | sort -r
  )
  for (( i = keep; i < ${#names[@]}; i++ )); do
    log "removing old snapshot ${names[i]} (keeping $keep)"
    rm -rf "${root:?}/${names[i]}"
  done
}

# confirm <expected-word> <yes-flag>
confirm() {
  local word="$1" yes="${2:-}"
  if [[ "$yes" == "--yes" ]]; then return 0; fi
  local answer
  if ! read -r -p "Type '$word' to proceed: " answer; then
    die "no confirmation input — aborting."
  fi
  [[ "$answer" == "$word" ]] || die "confirmation failed — aborting."
}
