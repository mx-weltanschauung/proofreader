#!/usr/bin/env bash
# Проверка бэкапа и восстановления в одноразовом окружении.
# Спек: docs/superpowers/specs/2026-08-04-verify-restore-design.md
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
load_config
# Источник сверки — местный SeaweedFS (зеркало, ключи и размеры объектов).
refuse_external_storage verify-restore.sh

# Источник — то, на что load_config навёл по умолчанию (.env / умолчания).
# Эти значения дальше не меняются: цель задаётся только префиксом окружения
# у конкретного вызова.
SRC_PG="$PG_CONTAINER"; SRC_SW="$SW_CONTAINER"; SRC_NET="$NET"
SRC_DB="$DB_NAME"; SRC_USER="$DB_USER"

V_NET=proofreader-verify-net
V_PG=proofreader-verify-postgres
V_SW=proofreader-verify-seaweedfs
V_BACKEND=proofreader-verify-backend
V_DB=proofreader
V_USER=proofreader
V_PASS=proofreader_password

SNAP=""; KEEP=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --snapshot) SNAP="${2:-}"; [[ -n "$SNAP" ]] || die "--snapshot требует путь"; shift 2 ;;
    --keep)     KEEP=1; shift ;;
    *) die "неизвестный аргумент: $1 (usage: verify-restore.sh [--snapshot <path>] [--keep])" ;;
  esac
done

# Предохранитель: цель обязана быть verify-шной и не совпадать с рабочим стеком.
for _n in "$V_PG" "$V_SW" "$V_BACKEND" "$V_NET"; do
  [[ "$_n" == *verify* ]] || die "целевое имя '$_n' не содержит 'verify' — отказ"
done
if [[ "$V_PG" == "$SRC_PG" || "$V_SW" == "$SRC_SW" || "$V_NET" == "$SRC_NET" ]]; then
  die "цель совпадает с рабочим стеком — отказ"
fi

# Коллизия с прошлым прогоном (--keep) — проверяем ДО установки trap, чтобы
# уборка не снесла окружение, которое оставили специально.
for _c in "$V_PG" "$V_SW" "$V_BACKEND"; do
  if docker inspect "$_c" >/dev/null 2>&1; then
    die "контейнер $_c уже существует (остался от прогона с --keep). Снести: docker rm -f -v $V_PG $V_SW $V_BACKEND; docker network rm $V_NET"
  fi
done
if docker network inspect "$V_NET" >/dev/null 2>&1; then
  die "сеть $V_NET уже существует. Снести: docker network rm $V_NET"
fi

WORK_DIR="$(mktemp -d)"

cleanup() {
  local rc=$?
  rm -rf "$WORK_DIR"
  if [[ -n "$KEEP" ]]; then
    log "окружение оставлено (--keep). Снести: docker rm -f -v $V_BACKEND $V_PG $V_SW; docker network rm $V_NET"
    return $rc
  fi
  log "уборка"
  docker rm -f -v "$V_BACKEND" "$V_PG" "$V_SW" >/dev/null 2>&1 || true
  docker network rm "$V_NET" >/dev/null 2>&1 || true
  return $rc
}
trap cleanup EXIT

# wait_ready <описание> <контейнер> <попыток> <команда...>
wait_ready() {
  local desc="$1" container="$2" tries="$3"; shift 3
  local i
  for ((i = 1; i <= tries; i++)); do
    if "$@" >/dev/null 2>&1; then log "$desc готов"; return 0; fi
    sleep 2
  done
  log "последние строки лога $container:"
  docker logs --tail 30 "$container" >&2 || true
  die "$desc не поднялся за $((tries * 2))с"
}

curl_in() { docker run --rm --network "$V_NET" curlimages/curl -sS "$@"; }
mc_src()  { NET="$SRC_NET" mc_run "$@"; }
mc_dst()  { NET="$V_NET"   mc_run "$@"; }

log "собираю образ бэкенда"
docker compose -f "$REPO_ROOT/docker-compose.yml" build backend >/dev/null

log "создаю сеть $V_NET"
docker network create "$V_NET" >/dev/null

log "поднимаю postgres"
docker run -d --name "$V_PG" --network "$V_NET" --network-alias postgres \
  -e POSTGRES_USER="$V_USER" -e POSTGRES_PASSWORD="$V_PASS" -e POSTGRES_DB="$V_DB" \
  postgres:15-alpine >/dev/null

log "поднимаю seaweedfs"
docker run -d --name "$V_SW" --network "$V_NET" --network-alias seaweedfs \
  -v "$REPO_ROOT/docker/seaweedfs/s3_config.json:/etc/seaweedfs/s3_config.json:ro" \
  chrislusf/seaweedfs:latest \
  server -dir=/data -s3 -s3.port=8333 \
  -s3.config=/etc/seaweedfs/s3_config.json -master.volumeSizeLimitMB=1024 >/dev/null

wait_ready "postgres"  "$V_PG"    30 docker exec "$V_PG" pg_isready -U "$V_USER"
wait_ready "seaweedfs" "$V_SW"    30 docker exec "$V_SW" wget -q -O /dev/null http://127.0.0.1:8333/healthz

log "окружение готово"

if [[ -z "$SNAP" ]]; then
  log "снимаю свежий бэкап рабочего стека"
  # backup.sh пишет логи в stderr, а путь снапшота — последней строкой stdout.
  SNAP="$("$REPO_ROOT/scripts/backup.sh" | tail -1)"
fi
[[ -d "$SNAP" ]] || die "снапшот не найден: $SNAP"
# restore.sh монтирует SNAP в docker -v; относительный путь docker воспримет
# как имя volume, а не как каталог на хосте — приводим к абсолютному.
SNAP="$(cd "$SNAP" && pwd)"
# Дамп лежит либо прямо в каталоге (старая раскладка), либо внутри meta.tar.
# Распаковкой и проверкой содержимого занимается restore.sh — здесь только ранний
# отсев заведомо пустого снапшота.
[[ -s "$SNAP/db.dump" || -s "$SNAP/meta.tar" ]] ||
  die "в снапшоте нет ни db.dump, ни meta.tar: $SNAP"
log "снапшот: $SNAP"

log "создаю бакет $S3_BUCKET в verify-хранилище"
mc_dst mb --ignore-existing "seaweed/$S3_BUCKET"

# Предохранитель от регрессии lib.sh. Вся безопасность вызова restore.sh ниже
# держится на том, что load_config уважает окружение вызывающего (приоритет
# «окружение > .env > умолчания»). Если этот механизм когда-нибудь сломается
# (откат правки, «упрощение» lib.sh, кто-то зашьёт PG_CONTAINER обратно) —
# restore.sh молча наведётся на рабочий Postgres/бакет. Существующий
# предохранитель на строках 33-38 сравнивает имена переменных этого скрипта,
# а не то, куда реально пойдёт restore.sh, и такую регрессию не ловит.
# Пробуем load_config в чистом подпроцессе с заведомо не рабочими значениями
# и требуем, чтобы они дошли до конца без замены на умолчания/.env.
log "проверяю, что load_config пропускает переопределение окружения"
PROBE="$(PG_CONTAINER=__probe__ NET=__probe__ bash -c \
  'source "'"$REPO_ROOT"'/scripts/lib.sh"; load_config; echo "$PG_CONTAINER|$NET"')"
[[ "$PROBE" == "__probe__|__probe__" ]] || \
  die "load_config не пропускает окружение вызывающего (получено: «$PROBE») — restore.sh пойдёт в рабочий стек. Отказ."

log "восстанавливаю снапшот в verify-окружение"
PG_CONTAINER="$V_PG" SW_CONTAINER="$V_SW" NET="$V_NET" \
  DB_NAME="$V_DB" DB_USER="$V_USER" \
  "$REPO_ROOT/scripts/restore.sh" "$SNAP" --yes

log "поднимаю бэкенд поверх восстановленной копии"
docker run -d --name "$V_BACKEND" --network "$V_NET" --network-alias backend \
  -e SERVER_HOST=0.0.0.0 -e SERVER_PORT=8080 \
  -e DB_HOST=postgres -e DB_PORT=5432 \
  -e DB_USER="$V_USER" -e DB_PASSWORD="$V_PASS" -e DB_NAME="$V_DB" -e DB_SSLMODE=disable \
  -e JWT_SECRET=dev-secret-change-me \
  -e ADMIN_EMAIL=admin@proofreader.local -e ADMIN_PASSWORD=admin \
  -e S3_ENDPOINT=http://seaweedfs:8333 \
  -e S3_PUBLIC_ENDPOINT=http://seaweedfs:8333 \
  -e S3_REGION=us-east-1 -e S3_BUCKET="$S3_BUCKET" \
  -e S3_ACCESS_KEY="$S3_KEY" -e S3_SECRET_KEY="$S3_SECRET" \
  -e S3_USE_PATH_STYLE=true -e S3_PRESIGN_TTL=30m \
  proofreader-backend >/dev/null

wait_ready "backend" "$V_BACKEND" 60 curl_in -o /dev/null -f http://backend:8080/api/health

log "копия развёрнута"

REPORT="$SNAP/verify-report.txt"
# Удаляем, а не обрезаем: в снапшоте из жёстких ссылок обрезание порезало бы оригинал.
rm -f "$REPORT"
CHECKS_PASS=0; CHECKS_FAIL=0; CHECKS_SKIP=0

report() { printf '%s\n' "$*" | tee -a "$REPORT" >&2; }
ok()     { CHECKS_PASS=$((CHECKS_PASS + 1)); report "OK   $*"; }
bad()    { CHECKS_FAIL=$((CHECKS_FAIL + 1)); report "FAIL $*"; }
skip()   { CHECKS_SKIP=$((CHECKS_SKIP + 1)); report "SKIP $*"; }
# check <описание> <ожидание> <факт>
check() {
  local desc="$1" want="$2" got="$3"
  if [[ "$want" == "$got" ]]; then ok "$desc ($got)"; else bad "$desc: ожидалось «$want», получено «$got»"; fi
}

# Сентинелы на отказ запроса. Без них при отказе ОБОИХ вызовов (например,
# миграция снесла таблицу из зашитого списка) check() сравнивает две пустые
# строки и молча печатает OK. err-src/err-dst никогда не равны друг другу и
# не равны реальным данным — любой отказ становится FAIL с говорящим текстом,
# а не ложным зелёным результатом.
psql_src() { docker exec "$SRC_PG" psql -U "$SRC_USER" -d "$SRC_DB" -tAc "$1" || echo "err-src"; }
psql_dst() { docker exec "$V_PG"   psql -U "$V_USER"   -d "$V_DB"   -tAc "$1" || echo "err-dst"; }

report "=== проверка снапшота $SNAP ==="
report "источник: $SRC_PG/$SRC_DB, копия: $V_PG/$V_DB"

report "--- база ---"

# Версия схемы против манифеста.
# `if VAR=$(...)` — единственная форма, где голое присваивание не убивает
# скрипт под set -e при ошибке: команда стоит условием if, а не сама по себе.
if MANIFEST_SCHEMA="$(grep -oP '"schema_version":\s*"\K[^"]+' "$SNAP/manifest.json")"; then
  check "версия схемы против манифеста" "$MANIFEST_SCHEMA" "$(psql_dst 'SELECT version FROM schema_migrations LIMIT 1')"
else
  bad "не удалось прочитать schema_version из манифеста $SNAP/manifest.json"
fi

# Список таблиц — именами, не числом.
TABLES_SQL="SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY 1"
check "список таблиц public" "$(psql_src "$TABLES_SQL" | tr '\n' ',')" "$(psql_dst "$TABLES_SQL" | tr '\n' ',')"

# Число строк по каждой таблице. query_to_xml позволяет посчитать все таблицы
# одним запросом, без зашитого списка имён.
COUNTS_SQL="SELECT table_name || '=' ||
  (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM public.%I', table_name), false, true, '')))[1]::text
  FROM information_schema.tables WHERE table_schema='public' ORDER BY 1"
# Голое присваивание VAR=$(...) — единственная форма, которую отслеживает
# set -e (в отличие от подстановки в аргументе, как в остальных проверках
# блока). Но с тех пор как psql_src/psql_dst сами гасят отказ сентинелом
# (`|| echo err-src`), функция всегда возвращает 0 — проверка по коду
# возврата больше ничего не ловит, поэтому смотрим на сам сентинел явно.
# COUNTS_OK гасит последующее сравнение: не сравниваем COUNTS_SRC с
# COUNTS_DST, если хоть один запрос не удался, иначе два пустых значения
# молча совпали бы и дали ложный OK.
COUNTS_OK=1
COUNTS_SRC="$(psql_src "$COUNTS_SQL")"
COUNTS_DST="$(psql_dst "$COUNTS_SQL")"
if [[ "$COUNTS_SRC" == "err-src" ]]; then bad "не удалось получить счётчики строк источника"; COUNTS_OK=""; fi
if [[ "$COUNTS_DST" == "err-dst" ]]; then bad "не удалось получить счётчики строк копии"; COUNTS_OK=""; fi
if [[ -n "$COUNTS_OK" ]]; then
  if [[ "$COUNTS_SRC" == "$COUNTS_DST" ]]; then
    ok "число строк во всех таблицах ($(printf '%s' "$COUNTS_SRC" | tr '\n' ' '))"
  else
    bad "число строк расходится:"
    # diff возвращает 1, когда находит расхождения — это не ошибка, а ожидаемый
    # результат сравнения; || true не даёт set -e прервать скрипт на этом месте.
    diff <(printf '%s\n' "$COUNTS_SRC") <(printf '%s\n' "$COUNTS_DST") | while read -r l; do report "     $l"; done || true
  fi
fi

# Контрольные суммы содержимого — ловят порчу текста, невидимую для счётчиков.
for t in works pages chapters page_versions documents users; do
  SUM_SQL="SELECT coalesce(md5(string_agg(md5(x.*::text), '' ORDER BY x.id)), 'empty') FROM $t x"
  check "контрольная сумма $t" "$(psql_src "$SUM_SQL")" "$(psql_dst "$SUM_SQL")"
done

# Последовательности: значение совпадает с источником И не отстаёт от max(id).
# Отставание — тихая поломка: данные на месте, а первая же вставка падает на
# дубликате первичного ключа.
SEQ_SQL="SELECT s.seq || '=' || coalesce(s.last_value::text, 'null') || '/' ||
  (xpath('/row/c/text()', query_to_xml(format('SELECT coalesce(max(%I),0) AS c FROM %s', s.col, s.tbl), false, true, '')))[1]::text
FROM (
  SELECT c.relname AS seq,
         pg_sequence_last_value(c.oid) AS last_value,
         d.refobjid::regclass::text AS tbl,
         a.attname AS col
  FROM pg_class c
  JOIN pg_depend d ON d.objid = c.oid AND d.classid = 'pg_class'::regclass AND d.refobjsubid > 0
  JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
  WHERE c.relkind = 'S' AND c.relnamespace = 'public'::regnamespace
) s ORDER BY 1"
check "последовательности совпадают с источником" "$(psql_src "$SEQ_SQL" | tr '\n' ' ')" "$(psql_dst "$SEQ_SQL" | tr '\n' ' ')"

# SEQ_ITER считает реальные обработанные строки. Раньше при отказе psql_dst
# подстановка давала пустую строку, `set -e` её не видел (см. комментарий
# выше про <<<), и цикл делал ноль итераций молча — одиннадцать проверок
# отставания последовательностей пропадали из отчёта без единого OK/FAIL.
# Теперь psql_dst при отказе отдаёт сентинел err-dst — непустую строку, так
# что цикл всё равно получает одну «строку» на вход; распознаём её явно,
# чтобы не упасть на арифметике `(( last >= maxid ))` с нечисловым maxid, и
# вдобавок страхуемся счётчиком на случай генуинно пустого списка
# последовательностей (успешный запрос, но ни одной строки).
SEQ_ITER=0
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  SEQ_ITER=$((SEQ_ITER + 1))
  if [[ "$line" == "err-src" || "$line" == "err-dst" ]]; then
    bad "последовательности: не удалось получить данные ($line) — отставание не проверялось"
    continue
  fi
  seq_name="${line%%=*}"; rest="${line#*=}"
  last="${rest%%/*}"; maxid="${rest##*/}"
  [[ "$last" == "null" ]] && last=0
  if [[ "$last" =~ ^[0-9]+$ && "$maxid" =~ ^[0-9]+$ ]]; then
    if (( last >= maxid )); then
      ok "последовательность $seq_name не отстаёт (last=$last, max=$maxid)"
    else
      bad "последовательность $seq_name отстаёт: last=$last, max(id)=$maxid — следующая вставка упадёт"
    fi
  else
    bad "последовательность $seq_name: не удалось разобрать значения (last=$last, max=$maxid)"
  fi
done <<< "$(psql_dst "$SEQ_SQL")"
if [[ "$SEQ_ITER" -eq 0 ]]; then
  bad "проверка отставания последовательностей не выполнилась ни разу — список последовательностей пуст или недоступен"
else
  ok "цикл проверки отставания последовательностей выполнился ($SEQ_ITER шт.)"
fi

report "--- хранилище ---"

S3_LIST_SRC="$WORK_DIR/s3-src.txt"
S3_LIST_DST="$WORK_DIR/s3-dst.txt"

# mc ls --json выдаёт по объекту на строку. Берём ключ и размер; ведущий
# «<бакет>/» срезается, если mc его добавил, — тогда ключ совпадает с тем, что
# лежит в pages.preview_path и works.file_path.
s3_list() {
  python3 -c '
import sys, json
bucket = sys.argv[1] + "/"
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    o = json.loads(line)
    if o.get("type") != "file":
        continue
    key = o["key"]
    if key.startswith(bucket):
        key = key[len(bucket):]
    print(key, o["size"])
' "$S3_BUCKET" | sort
}

# LIST_OK гасится, если хотя бы один из листингов не удался. Пустой файл от
# упавшего mc ls неотличим по содержимому от «реально нет объектов» — без
# этого флага wc -l/diff по двум пустым файлам молча дали бы ложный OK
# (сравнение двух пустых значений не должно проходить как совпадение).
LIST_OK=1
if ! mc_src ls --recursive --json "seaweed/$S3_BUCKET" | s3_list > "$S3_LIST_SRC"; then
  bad "не удалось получить список объектов источника через mc_src"
  LIST_OK=""
fi
if ! mc_dst ls --recursive --json "seaweed/$S3_BUCKET" | s3_list > "$S3_LIST_DST"; then
  bad "не удалось получить список объектов копии через mc_dst"
  LIST_OK=""
fi

if [[ -n "$LIST_OK" ]]; then
  if ! N_SRC="$(wc -l < "$S3_LIST_SRC" | tr -d ' ')"; then bad "не удалось посчитать объекты в списке источника"; N_SRC="err-src"; fi
  if ! N_DST="$(wc -l < "$S3_LIST_DST" | tr -d ' ')"; then bad "не удалось посчитать объекты в списке копии"; N_DST="err-dst"; fi
  if ! N_MANIFEST="$(grep -oP '"s3_object_count":\s*\K[0-9]+' "$SNAP/manifest.json")"; then bad "не удалось прочитать s3_object_count из манифеста $SNAP/manifest.json"; N_MANIFEST="err-manifest"; fi
  check "число объектов против источника" "$N_SRC" "$N_DST"
  check "число объектов против манифеста" "$N_MANIFEST" "$N_DST"

  if diff -q "$S3_LIST_SRC" "$S3_LIST_DST" >/dev/null; then
    ok "ключи и размеры всех $N_DST объектов совпадают"
  else
    bad "ключи/размеры расходятся:"
    diff "$S3_LIST_SRC" "$S3_LIST_DST" | head -40 | while read -r l; do report "     $l"; done || true
  fi
else
  bad "число объектов против источника: список объектов не получен — сравнение не проводилось"
  bad "число объектов против манифеста: список объектов не получен — сравнение не проводилось"
  bad "ключи и размеры объектов: список объектов не получен — сравнение не проводилось"
fi

# Побайтовая сверка выборки: первый, последний и восемь равномерно по списку,
# плюс принудительно один оригинал (не .png) и одна превью (.png).
SAMPLE="$WORK_DIR/sample.txt"
awk 'BEGIN{c=0}
  {c++; keys[c]=$1}
  END{
    if (c == 0) exit
    step = (c > 10) ? int(c/9) : 1
    for (i = 1; i <= c; i += step) print keys[i]
    print keys[c]
  }' "$S3_LIST_DST" | sort -u > "$SAMPLE"
grep -m1 '\.png$'  "$S3_LIST_DST" | awk '{print $1}' >> "$SAMPLE" || true
grep -m1 -v '\.png$' "$S3_LIST_DST" | awk '{print $1}' >> "$SAMPLE" || true
sort -u -o "$SAMPLE" "$SAMPLE"

if [[ -s "$SAMPLE" ]]; then
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! MD5_SRC="$(mc_src cat "seaweed/$S3_BUCKET/$key" | md5sum | cut -d' ' -f1)"; then
      bad "не удалось прочитать $key из источника"; MD5_SRC="err-src"
    fi
    if ! MD5_DST="$(mc_dst cat "seaweed/$S3_BUCKET/$key" | md5sum | cut -d' ' -f1)"; then
      bad "не удалось прочитать $key из копии"; MD5_DST="err-dst"
    fi
    check "содержимое $key" "$MD5_SRC" "$MD5_DST"
  done < "$SAMPLE"
else
  # Пустая выборка — либо список объектов копии не получен (LIST_OK погашен
  # выше), либо он реально пуст. В обоих случаях побайтовая сверка не
  # проводилась и это не должно проходить молча.
  bad "побайтовая сверка выборки: список объектов копии пуст или недоступен — сверка не проводилась"
fi

report "--- API ---"

api_code() { curl_in -o /dev/null -w '%{http_code}' "$@"; }

check "GET /api/health" "200" "$(api_code http://backend:8080/api/health)"

# Как и с mc ls/cat выше: curl_in — обычная команда, её отказ (не HTTP-код,
# а сам обрыв соединения) под set -e молча убьёт скрипт, если стоять голым
# оператором. Оборачиваем в if, чтобы отказ шёл через bad().
WORKS_JSON="$WORK_DIR/works.json"
if ! curl_in "http://backend:8080/api/works?limit=1000" > "$WORKS_JSON"; then
  bad "не удалось получить /api/works — число работ не сверялось"
elif ! WORKS_COUNT="$(python3 -c 'import sys,json; print(len(json.load(open(sys.argv[1]))))' "$WORKS_JSON")"; then
  bad "не удалось разобрать ответ /api/works как JSON"
else
  # Каталог отдаёт только тома верхнего уровня: WorkRepository.List фильтрует
  # `WHERE parent_work_id IS NULL`, поэтому служебные работы-дети (role =
  # front_matter, сканы титула/содержания рядом с томом) в ответ не попадают.
  # Сравнивать с голым count(*) по works нельзя — на любом томе со служебной
  # работой это расхождение на ровном месте.
  check "число работ в /api/works" \
    "$(psql_dst 'SELECT count(*) FROM works WHERE parent_work_id IS NULL')" "$WORKS_COUNT"
fi

# Превью сверяется только у полосы, чей ключ лежит в копии: локальный бакет
# держит лишь рабочий набор томов (спека 2026-10-03-local-scans-working-set),
# и полоса «наименьшего тома» почти наверняка выселена. Пустой бакет —
# пропуск, а не ok: проверка превью не проводилась. Порядок прежний: наименьший
# work_id среди страниц с непустым текстом и ключом превью, внутри — номер.
PREVIEW_CHECK=1
PREVIEW_WHY=""
if [[ -z "$LIST_OK" ]]; then
  PREVIEW_CHECK=""; PREVIEW_WHY="список объектов копии не получен"
elif [[ ! -s "$S3_LIST_DST" ]]; then
  PREVIEW_CHECK=""; PREVIEW_WHY="локальный бакет копии пуст (рабочий набор пуст)"
fi
if ! CANDIDATES="$(psql_dst "SELECT work_id || '|' || id || '|' || page_number || '|' || preview_path
  FROM pages
  WHERE coalesce(content_markdown,'') <> '' AND coalesce(preview_path,'') <> ''
  ORDER BY work_id, page_number")" || [[ "$CANDIDATES" == err-dst ]]; then
  PICK="err-dst"
elif [[ -n "$PREVIEW_CHECK" ]]; then
  PICK="$(pick_listed_page "$S3_LIST_DST" <<<"$CANDIDATES")"
  if [[ -z "$PICK" ]]; then
    PREVIEW_CHECK=""
    PREVIEW_WHY="ни один ключ превью из базы не лежит в копии бакета"
    PICK="$(head -1 <<<"$CANDIDATES")"
  fi
else
  PICK="$(head -1 <<<"$CANDIDATES")"
fi
if [[ "$PICK" == err-dst ]]; then
  bad "не удалось выбрать работу/страницу для смока — запрос к копии базы не удался"
elif [[ -z "$PICK" ]]; then
  # Не die: остальные проверки блока (health, число работ) уже прошли и не
  # должны пропасть из отчёта из-за одного пустого выбора. Как и везде в
  # блоке, отсутствие данных для сверки — это провал проверки, а не повод
  # прервать весь прогон.
  bad "в копии нет ни одной страницы с текстом и превью — сверка страницы/превью/рендера/оригинала не проводилась"
else
  IFS='|' read -r WID PID PNUM PPATH <<< "$PICK"
  report "выбрана работа $WID, страница $PID (номер $PNUM)"

  check "GET /api/works/$WID/pages" "200" "$(api_code "http://backend:8080/api/works/$WID/pages")"

  PAGE_JSON="$WORK_DIR/page.json"
  if ! curl_in "http://backend:8080/api/works/$WID/pages/$PID" > "$PAGE_JSON"; then
    bad "не удалось получить /api/works/$WID/pages/$PID — content_markdown/preview_url не сверялись"
  else
    if ! PAGE_MD_LEN="$(python3 -c 'import sys,json; print(len(json.load(open(sys.argv[1])).get("content_markdown","")))' "$PAGE_JSON")"; then
      bad "не удалось разобрать ответ /api/works/$WID/pages/$PID как JSON"
    elif [[ "$PAGE_MD_LEN" -gt 0 ]]; then
      ok "content_markdown страницы непустой ($PAGE_MD_LEN символов)"
    else
      bad "content_markdown страницы пустой"
    fi

    if [[ -z "$PREVIEW_CHECK" ]]; then
      skip "превью не сверялись: $PREVIEW_WHY"
    else
      if ! PREVIEW_URL="$(python3 -c 'import sys,json; print(json.load(open(sys.argv[1])).get("preview_url",""))' "$PAGE_JSON")"; then
        bad "не удалось прочитать preview_url из ответа /api/works/$WID/pages/$PID"
      elif [[ -z "$PREVIEW_URL" ]]; then
        bad "preview_url не отдан — presign не сработал"
      else
        check "код ответа preview_url" "200" "$(api_code "$PREVIEW_URL")"
        PREVIEW_BIN="$WORK_DIR/preview.bin"
        if ! curl_in "$PREVIEW_URL" > "$PREVIEW_BIN"; then
          bad "не удалось скачать preview_url — сигнатура и размер превью не сверялись"
        else
          # Сигнатура PNG: 89 50 4E 47
          check "сигнатура PNG у превью" "89504e47" "$(head -c 4 "$PREVIEW_BIN" | od -An -tx1 | tr -d ' \n')"
          if ! PREVIEW_S3_SIZE="$(awk -v k="$PPATH" '$1 == k {print $2}' "$S3_LIST_DST")"; then
            bad "не удалось найти $PPATH в списке объектов копии"
          elif [[ -z "$PREVIEW_S3_SIZE" ]]; then
            bad "размер превью совпадает с объектом в S3: объект $PPATH не найден в списке копии"
          else
            check "размер превью совпадает с объектом в S3" "$PREVIEW_S3_SIZE" "$(stat -c%s "$PREVIEW_BIN")"
          fi
        fi
      fi
    fi
  fi

  WFILE="$(psql_dst "SELECT coalesce(file_path, '') FROM works WHERE id = $WID")"
  if [[ "$WFILE" == err-dst ]]; then
    bad "не удалось прочитать file_path работы $WID — запрос к копии базы не удался"
  elif [[ -z "$WFILE" ]] || ! listing_has_key "$S3_LIST_DST" "$WFILE"; then
    skip "оригинал работы $WID не сверялся: его нет в локальном бакете (том выселен или без оригинала)"
  else
    WORK_JSON="$WORK_DIR/work.json"
    if ! curl_in "http://backend:8080/api/works/$WID" > "$WORK_JSON"; then
      bad "не удалось получить /api/works/$WID — file_url не сверялся"
    elif ! FILE_URL="$(python3 -c 'import sys,json; print(json.load(open(sys.argv[1])).get("file_url",""))' "$WORK_JSON")"; then
      bad "не удалось прочитать file_url из ответа /api/works/$WID"
    elif [[ -z "$FILE_URL" ]]; then
      bad "file_url работы не отдан"
    else
      check "код ответа file_url" "200" "$(api_code "$FILE_URL")"
      if ! FILE_BYTES="$(curl_in "$FILE_URL" | wc -c | tr -d ' ')"; then
        bad "не удалось скачать оригинал работы по file_url"
      elif [[ "$FILE_BYTES" -gt 0 ]]; then
        ok "оригинал работы скачивается ($FILE_BYTES байт)"
      else
        bad "оригинал работы скачался пустым"
      fi
    fi
  fi

  RENDER_URL="http://backend:8080/api/works/$WID/pages/$PID/render"
  check "код ответа рендера страницы" "200" "$(api_code "$RENDER_URL")"
  if ! RENDER_BYTES="$(curl_in "$RENDER_URL" | wc -c | tr -d ' ')"; then
    bad "не удалось скачать рендер страницы"
  elif [[ "$RENDER_BYTES" -gt 0 ]]; then
    ok "рендер страницы непустой ($RENDER_BYTES байт)"
  else
    bad "рендер страницы пустой"
  fi
fi

report "--- итог ---"
report "пройдено: $CHECKS_PASS, провалено: $CHECKS_FAIL, пропущено: $CHECKS_SKIP"
log "отчёт: $REPORT"
[[ "$CHECKS_FAIL" -eq 0 ]] || exit 1
