#!/usr/bin/env bash
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
load_config
refuse_external_storage reset.sh

YES=""; DO_BACKUP=""
for arg in "$@"; do
  case "$arg" in
    --yes) YES="--yes" ;;
    --backup) DO_BACKUP="1" ;;
    *) die "unknown arg: $arg (usage: reset.sh [--yes] [--backup])" ;;
  esac
done

require_running "$PG_CONTAINER"
require_running "$SW_CONTAINER"

log "This will PERMANENTLY DELETE all DB data and all S3 files."
confirm proofreader "$YES"

if [[ -n "$DO_BACKUP" ]]; then
  log "taking a safety backup first"
  "$REPO_ROOT/scripts/backup.sh"
fi

log "dropping and recreating public schema"
docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -c \
  "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

log "applying migrations"
docker compose -f "$REPO_ROOT/docker-compose.yml" build migrate
docker compose -f "$REPO_ROOT/docker-compose.yml" run --rm migrate

LATEST=$(ls "$REPO_ROOT"/internal/database/migrations/*.up.sql | sed -E 's|.*/0*([0-9]+)_.*|\1|' | sort -n | tail -1)
APPLIED=$(docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc "SELECT version FROM schema_migrations")
[[ "$APPLIED" == "$LATEST" ]] || die "schema at migration $APPLIED but latest on disk is $LATEST — migrate image may be stale"
log "schema at migration $APPLIED (latest)"

log "emptying S3 bucket"
BEFORE=$(mc_run ls --recursive "seaweed/$S3_BUCKET" | wc -l | tr -d ' ')
if [[ "$BEFORE" != "0" ]]; then
  mc_run rm --recursive --force "seaweed/$S3_BUCKET"
fi
REMAIN=$(mc_run ls --recursive "seaweed/$S3_BUCKET" | wc -l | tr -d ' ')
[[ "$REMAIN" == "0" ]] || die "S3 bucket not empty after rm ($REMAIN objects remain)"

log "reset complete: empty schema at latest migration + empty bucket. Admin re-seeds on next backend start."
