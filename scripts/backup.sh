#!/usr/bin/env bash
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
load_config
refuse_external_storage backup.sh

require_running "$PG_CONTAINER"
require_running "$SW_CONTAINER"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEST="$BACKUP_ROOT/proofreader-$STAMP"
mkdir -p "$DEST/files"
touch "$DEST/.INCOMPLETE"
trap 'die "backup failed; left $DEST/.INCOMPLETE in place"' ERR

log "dumping database -> db.dump"
docker exec "$PG_CONTAINER" pg_dump -U "$DB_USER" -Fc "$DB_NAME" > "$DEST/db.dump"

log "bundling git repository -> repo.bundle"
# Бандл — вся история одним файлом, разворачивается обычным `git clone repo.bundle`.
# Отсутствие репозитория не повод ронять бэкап базы: пишем предупреждение и идём дальше.
if git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1 &&
   git -C "$REPO_ROOT" bundle create "$DEST/repo.bundle" --all; then
  BUNDLE_SIZE="$(stat -c%s "$DEST/repo.bundle")"
else
  log "WARNING: git bundle не создан — снапшот останется без repo.bundle"
  rm -f "$DEST/repo.bundle"
  BUNDLE_SIZE=0
fi

log "mirroring S3 bucket -> files/"
MC_MOUNT="$DEST" mc_run mirror "seaweed/$S3_BUCKET" /data/files

log "writing manifest.json"
DUMP_SIZE="$(stat -c%s "$DEST/db.dump")"
OBJ_COUNT="$(mc_run ls --recursive "seaweed/$S3_BUCKET" | wc -l | tr -d ' ')"
SCHEMA_VER="$(docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  'SELECT version FROM schema_migrations LIMIT 1' 2>/dev/null || echo unknown)"
GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
PG_IMAGE="$(docker inspect -f '{{.Config.Image}}' "$PG_CONTAINER")"
SW_IMAGE="$(docker inspect -f '{{.Config.Image}}' "$SW_CONTAINER")"
cat > "$DEST/manifest.json" <<JSON
{
  "timestamp": "$STAMP",
  "git_commit": "$GIT_COMMIT",
  "pg_image": "$PG_IMAGE",
  "seaweedfs_image": "$SW_IMAGE",
  "schema_version": "$SCHEMA_VER",
  "db_dump_bytes": $DUMP_SIZE,
  "git_bundle_bytes": $BUNDLE_SIZE,
  "s3_object_count": $OBJ_COUNT
}
JSON

# Архив — только лёгкая часть снапшота. files/ остаётся каталогом: mc mirror ходит
# в него инкрементно, restore.sh и ручная доливка одного тома работают как раньше.
# manifest.json тоже остаётся снаружи — его читают грепом, не распаковывая архив.
# Сжатия нет намеренно: pg_dump -Fc и packfile бандла уже сжаты zlib, второй проход
# даст доли процента за заметное время.
log "packing db.dump + repo.bundle -> meta.tar"
META_MEMBERS=(db.dump)
[[ -f "$DEST/repo.bundle" ]] && META_MEMBERS+=(repo.bundle)
tar -C "$DEST" -cf "$DEST/meta.tar" "${META_MEMBERS[@]}"
rm -f "$DEST/db.dump" "$DEST/repo.bundle"

rm -f "$DEST/.INCOMPLETE"
trap - ERR

# Чистка — только после успеха: упавший бэкап не должен уносить старые снапшоты.
prune_backups "$BACKUP_ROOT" "${BACKUP_KEEP:-3}"

log "backup complete: $DEST (db.dump ${DUMP_SIZE}B, bundle ${BUNDLE_SIZE}B, $OBJ_COUNT objects)"
echo "$DEST"
