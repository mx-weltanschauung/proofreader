#!/usr/bin/env bash
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
load_config

TARGET=""; YES=""; DB_ONLY=""
for arg in "$@"; do
  case "$arg" in
    --yes) YES="--yes" ;;
    # Только база: сканы уже лежат во внешнем хранилище (bootstrap.sh
    # --external-s3), а снапшот приехал без files/.
    --db-only) DB_ONLY=1 ;;
    -*) die "неизвестный ключ: $arg (usage: restore.sh <path|latest> [--yes] [--db-only])" ;;
    *) [[ -z "$TARGET" ]] || die "лишний аргумент: $arg"; TARGET="$arg" ;;
  esac
done
[[ -n "$TARGET" ]] || die "usage: restore.sh <path|latest> [--yes] [--db-only]"
[[ -n "$DB_ONLY" ]] || refuse_external_storage restore.sh

if [[ "$TARGET" == "latest" ]]; then
  SNAP="$(ls -dt "$BACKUP_ROOT"/proofreader-* 2>/dev/null | while read -r d; do
    [[ -e "$d/.INCOMPLETE" ]] || { echo "$d"; break; }; done)"
  [[ -n "$SNAP" ]] || die "no complete snapshot found under $BACKUP_ROOT"
else
  SNAP="$TARGET"
fi

[[ -d "$SNAP" ]]                || die "snapshot dir not found: $SNAP"
[[ ! -e "$SNAP/.INCOMPLETE" ]]  || die "snapshot is marked .INCOMPLETE: $SNAP"
[[ -n "$DB_ONLY" || -d "$SNAP/files" ]] || die "missing files/ in $SNAP"

# Дамп достаём до всяких разрушительных действий: снапшот без него должен
# отваливаться раньше, чем мы тронем базу. Старые снапшоты (db.dump лежит
# в каталоге) читаются как есть, новые — распаковываются из meta.tar.
META_WORK="$(mktemp -d)"
trap 'rm -rf "$META_WORK"' EXIT
META="$(snapshot_meta "$SNAP" "$META_WORK")"

require_running "$PG_CONTAINER"
[[ -n "$DB_ONLY" ]] || require_running "$SW_CONTAINER"

if [[ -n "$DB_ONLY" ]]; then
  log "About to OVERWRITE current DB from: $SNAP (--db-only: хранилище не трогаю)"
else
  log "About to OVERWRITE current DB and S3 bucket from: $SNAP"
  log "target: $PG_CONTAINER/$DB_NAME, bucket seaweed/$S3_BUCKET on net $NET"
fi
confirm proofreader "$YES"

log "restoring database"
docker exec -i "$PG_CONTAINER" pg_restore --clean --if-exists --single-transaction -U "$DB_USER" -d "$DB_NAME" < "$META/db.dump"

if [[ -n "$DB_ONLY" ]]; then
  TBL_NOW="$(docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
  log "restore complete (--db-only): public tables $TBL_NOW"
  exit 0
fi

log "restoring S3 bucket (mirror with --remove)"
MC_MOUNT="$SNAP" mc_run mirror --overwrite --remove /data/files "seaweed/$S3_BUCKET"

log "verifying against manifest"
OBJ_NOW="$(mc_run ls --recursive "seaweed/$S3_BUCKET" | wc -l | tr -d ' ')"
OBJ_EXPECT="$(grep -oP '"s3_object_count":\s*\K[0-9]+' "$SNAP/manifest.json")"
TBL_NOW="$(docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")"
log "restore complete: S3 objects $OBJ_NOW (expected $OBJ_EXPECT), public tables $TBL_NOW"
[[ "$OBJ_NOW" == "$OBJ_EXPECT" ]] || log "WARNING: object count mismatch"
