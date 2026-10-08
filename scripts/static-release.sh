#!/usr/bin/env bash
# static-release.sh — статическая читальня: сборка из локальной базы, индекс
# поиска по тексту, сервер для архива, самопроверка, zip.
# Спека: docs/superpowers/specs/2026-10-06-static-reading-room-design.md,
# выкладка — docs/superpowers/specs/2026-10-07-static-reading-room-publish-design.md
#
#   scripts/static-release.sh [каталог]          # весь корпус → static/
#   STATIC_EDITION=7 scripts/static-release.sh   # одно издание (замер)
#
# В архив идут только работы, которые уже есть на боевом (GET /api/works с
# PROD_API_URL): архив не должен опережать сайт. Рядом с zip пишется
# chitalnya-<дата>.build.json — состав сборки для static-publish.sh.
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Живая читальня, с которой сверяется состав (её адрес, например
# https://lib.example.org). Умолчания нет: сверка с чужим сайтом молча
# выпустила бы архив не того состава. Тот же PROD_API_URL, что у takedown.sh.
PROD_API_URL="${PROD_API_URL:-}"
# Потолок списка работ: GET /api/works режет по limit, и обрезанный список
# молча выкинул бы тома из архива.
PROD_WORKS_LIMIT=100000

# fetch_prod_works <файл> — список работ боевого (каталог без передних
# листов) в файл. Сбой, пустой список и список, упёршийся в потолок, — отказ.
# prod_base — адрес живой читальни без косой в конце: на него ведут ссылки
# «эта страница в читальне онлайн», им же самопроверка ограничивает внешние
# ссылки. Берётся из PROD_API_URL, а не из PUBLIC_BASE_URL локального .env:
# тот смотрит на локальный бэкенд (или пуст), и архив молча терял бы ссылки.
prod_base() { printf '%s' "${PROD_API_URL%/}"; }

fetch_prod_works() {
  local out="$1" n
  curl -fsS --max-time 60 -o "$out" "$PROD_API_URL/api/works?limit=$PROD_WORKS_LIMIT" ||
    die "не удалось получить список работ боевого ($PROD_API_URL/api/works)"
  n="$(jq 'if type == "array" then length else error("не массив") end' "$out")" ||
    die "список работ боевого не разобран: $out"
  (( n > 0 )) || die "боевой отдал пустой список работ — собирать нечего"
  (( n < PROD_WORKS_LIMIT )) || die "список работ боевого упёрся в потолок $PROD_WORKS_LIMIT"
}

# fetch_apparatus_pages <static-exclude.txt> <файл> — полосы боевого у работ
# со снятым аппаратом («N apparatus», кроме снятых целиком): {"N": [номера…]}
# из GET /api/works/N/page-map. Самопроверка (-prod-pages) сверяет с ними
# сборку: аппарат вырезан по локальной разметке is_apparatus, а снимали его на
# боевом по своей, и разойтись они могут. Нет таких работ — пустой объект.
fetch_apparatus_pages() {
  local exclude="$1" out="$2" one="$2.one" acc='{}' id
  for id in $(sed 's/#.*//' "$exclude" |
    awk 'NF == 1 { whole[$1] = 1 } NF == 2 && $2 == "apparatus" { app[$1] = 1 }
         END { for (i in app) if (!(i in whole)) print i }'); do
    curl -fsS --max-time 60 -o "$one" "$PROD_API_URL/api/works/$id/page-map" ||
      die "полосы работы $id на боевом не получены ($PROD_API_URL/api/works/$id/page-map)"
    acc="$(jq -c --arg id "$id" --slurpfile p "$one" '. + {($id): [$p[0][].page_number]}' <<<"$acc")" ||
      die "полосы работы $id на боевом не разобраны"
  done
  rm -f "$one"
  printf '%s\n' "$acc" > "$out"
}

# build_json <дата> <коммит> <zip> <число файлов> <состав> <исключения> —
# сведения о сборке (chitalnya-<дата>.build.json). <состав> — файл -built
# генератора: {catalog_ids, work_ids}. sha256 списка исключений, с которым
# собран архив, сверяет static-publish.sh: сборка, собранная до снятия, не
# должна уехать в бакет после него.
build_json() {
  local date="$1" commit="$2" zip="$3" files="$4" built="$5" exclude="$6"
  jq -n --arg date "$date" --arg commit "$commit" --arg file "$(basename "$zip")" \
    --argjson size "$(stat -c %s "$zip")" \
    --arg sha256 "$(sha256sum "$zip" | cut -d' ' -f1)" \
    --arg md5 "$(md5sum "$zip" | cut -d' ' -f1)" \
    --argjson files "$files" --slurpfile built "$built" \
    --arg exclude_sha256 "$(sha256sum "$exclude" | cut -d' ' -f1)" \
    '{format: 1, file: $file, date: $date, commit: $commit, size: $size,
      sha256: $sha256, md5: $md5, files: $files,
      works: ($built[0].catalog_ids | length),
      catalog_ids: $built[0].catalog_ids, work_ids: $built[0].work_ids,
      exclude_sha256: $exclude_sha256}'
}

main() {
  load_config
  [[ -n "$PROD_API_URL" ]] || die "задайте PROD_API_URL — адрес живой читальни, с которой сверяется состав"

  local DATE NAME DEST OUT PARTIAL EXCLUDE PROD BUILT PRODPAGES
  DATE="$(date -u +%F)"
  NAME="chitalnya-$DATE"
  DEST="${1:-$REPO_ROOT/static}"
  DEST="${DEST%/}"
  OUT="$DEST/$NAME"
  PARTIAL="$OUT.partial"
  # Снятые по жалобе тома в архив не идут — см. комментарий в самом файле.
  # Список — файл экземпляра; его нет — снятых нет.
  EXCLUDE="${STATIC_EXCLUDE:-$REPO_ROOT/instance/static-exclude.txt}"
  [[ -f "$EXCLUDE" ]] || EXCLUDE=/dev/null
  PROD="$DEST/$NAME.prod-works.json"
  BUILT="$DEST/$NAME.built.json"
  PRODPAGES="$DEST/$NAME.prod-pages.json"

  command -v zip >/dev/null || die "zip не найден (apt install zip)"
  command -v jq >/dev/null || die "jq не найден (apt install jq)"
  [[ -x "$REPO_ROOT/frontend/node_modules/.bin/pagefind" ]] || die "pagefind не установлен: cd frontend && npm install"
  [[ ! -e "$OUT" ]] || die "$OUT уже есть — удалите его или соберите в другой каталог"

  local edition_flag=()
  if [[ -n "${STATIC_EDITION:-}" ]]; then
    edition_flag=(-edition "$STATIC_EDITION")
  fi

  rm -rf "$PARTIAL"
  mkdir -p "$DEST"

  log "состав боевого: $PROD_API_URL"
  fetch_prod_works "$PROD"
  fetch_apparatus_pages "$EXCLUDE" "$PRODPAGES"

  log "генерация → $PARTIAL"
  (cd "$REPO_ROOT" && go run ./cmd/staticsite build -out "$PARTIAL" -date "$DATE" -base "$(prod_base)" -exclude "$EXCLUDE" \
    -only "$PROD" -built "$BUILT" "${edition_flag[@]}")

  log "индекс поиска по тексту"
  (cd "$REPO_ROOT/frontend" && npx --no-install pagefind --site "$PARTIAL")

  log "сервер для архива"
  local target platform name
  for target in linux/amd64:serve-linux windows/amd64:serve-windows.exe darwin/arm64:serve-macos darwin/amd64:serve-macos-intel; do
    platform="${target%%:*}"
    name="${target#*:}"
    (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS="${platform%/*}" GOARCH="${platform#*/}" \
      go build -trimpath -ldflags='-s -w' -o "$PARTIAL/$name" ./cmd/staticserve)
  done

  log "самопроверка"
  (cd "$REPO_ROOT" && go run ./cmd/staticsite check -allow "$(prod_base)/" -exclude "$EXCLUDE" -only "$PROD" -prod-pages "$PRODPAGES" "${edition_flag[@]}" "$PARTIAL")

  mv "$PARTIAL" "$OUT"
  log "архив"
  (cd "$DEST" && rm -f "$NAME.zip" "$NAME.zip.sha256" \
    && zip -qr -X "$NAME.zip" "$NAME" \
    && sha256sum "$NAME.zip" > "$NAME.zip.sha256")
  build_json "$DATE" "$(git -C "$REPO_ROOT" rev-parse --short HEAD)" "$DEST/$NAME.zip" \
    "$(find "$OUT" -type f | wc -l)" "$BUILT" "$EXCLUDE" > "$DEST/$NAME.build.json"
  rm -f "$PROD" "$BUILT" "$PRODPAGES"
  log "готово: $DEST/$NAME.zip ($(du -h "$DEST/$NAME.zip" | cut -f1)), файлов: $(find "$OUT" -type f | wc -l), работ каталога: $(jq .works "$DEST/$NAME.build.json")"
}

# Источается тестом — тогда main не запускается.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
