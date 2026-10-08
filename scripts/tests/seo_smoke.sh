#!/usr/bin/env bash
# Смок поисковой оптимизации против живого стека.
#
# Проверяет то, чего не проверяют тесты Go: что nginx действительно развёл
# краулера и читателя. Требует поднятого docker compose (фронт на 3100).
#
#   ./scripts/tests/seo_smoke.sh [базовый-адрес]
set -uo pipefail
# Без -e: скрипт сам решает, что делать с неудачей каждой проверки (fail()),
# а не падает на первом же curl с ненулевым кодом — иначе непрошедшие
# проверки после первого сбоя не выполняются вообще, и оператор вместо
# списка расхождений видит голый "curl: (22)".

BASE="${1:-http://localhost:3100}"
BOT='TelegramBot (like TwitterBot)'
HUMAN='Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0'

failures=0

fail() { printf 'ПРОВАЛ: %s\n' "$1" >&2; failures=$((failures + 1)); }
ok()   { printf 'ок: %s\n' "$1"; }

# Забрать тело ответа, не роняя скрипт: сетевая ошибка или код 4xx/5xx дают
# пустую строку, а не завершают выполнение. Дальше каждая проверка сама
# решает, что значит пустой результат.
fetch() { curl -fsS "$@" 2>/dev/null || true; }

# Первый том, который отдаёт полка: смок не должен зависеть от того, какие
# идентификаторы оказались в базе.
work_id="$(fetch "$BASE/api/shelf" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin);
eds=d.get("editions") or []
print((eds[0]["volumes"][0]["id"]) if eds and eds[0].get("volumes") else "")' 2>/dev/null || true)"

if [ -z "$work_id" ]; then
  fail 'в читальне нет ни одного тома — смок проверять нечего'
  exit 1
fi
ok "проба на работе $work_id"

bot_html="$(fetch -A "$BOT" "$BASE/works/$work_id")"
human_html="$(fetch -A "$HUMAN" "$BASE/works/$work_id")"

if grep -q 'property="og:title"' <<<"$bot_html"; then
  ok 'краулер получил og:title'
else
  fail 'краулер не получил og:title'
fi

if grep -q 'property="og:image"' <<<"$bot_html"; then
  ok 'краулер получил og:image'
else
  fail 'краулер не получил og:image'
fi

if grep -q 'rel="canonical"' <<<"$bot_html"; then
  ok 'краулер получил canonical'
else
  fail 'краулер не получил canonical'
fi

if grep -q 'id="root"' <<<"$human_html"; then
  ok 'читатель получил SPA'
else
  fail 'читатель не получил SPA'
fi

if grep -q 'og:title' <<<"$human_html"; then
  fail 'читателю уехала страница краулера'
else
  ok 'читателю не досталась страница краулера'
fi

# Полоса: превью есть, индексации нет.
read_html="$(fetch -A "$BOT" "$BASE/works/$work_id/read/1")"
if grep -q 'content="noindex,follow"' <<<"$read_html"; then
  ok 'полоса отдана с noindex,follow'
else
  fail 'полоса отдана без noindex'
fi

# Абсолютные адреса обязаны вести на этот же хост, а не на localhost:3100 из
# дефолта PUBLIC_BASE_URL: иначе поисковик уйдёт индексировать localhost.
host="${BASE#*://}"
canonical="$(grep -o 'rel="canonical" href="[^"]*"' <<<"$bot_html" | head -1 || true)"
if [ -n "$canonical" ] && grep -q "$host" <<<"$canonical"; then
  ok "canonical ведёт на $host"
else
  fail "canonical ведёт не на $host: '$canonical' (проверьте PUBLIC_BASE_URL)"
fi

# robots и карта.
robots="$(fetch "$BASE/robots.txt")"
if grep -q '^Sitemap: ' <<<"$robots"; then
  ok 'в robots.txt есть строка Sitemap'
else
  fail 'в robots.txt нет строки Sitemap'
fi

if grep -q 'Disallow: /login' <<<"$robots"; then
  ok 'в robots.txt закрыт /login'
else
  fail 'в robots.txt открыт /login'
fi

sitemap_xml="$(fetch "$BASE/sitemap.xml")"
if [ -n "$sitemap_xml" ] \
  && python3 -c 'import sys,xml.etree.ElementTree as E; E.fromstring(sys.stdin.read())' \
     <<<"$sitemap_xml" 2>/dev/null; then
  ok 'индекс карты сайта разбирается как XML'
else
  fail 'индекс карты сайта не разбирается как XML'
fi

# Карточка превью. Вид в адресе — единственное число: /og/work/<id>.png.
card_type="$(curl -fsS -o /dev/null -w '%{content_type}' "$BASE/og/work/$work_id.png" 2>/dev/null || true)"
if [ "$card_type" = "image/png" ]; then
  ok 'карточка отдана как image/png'
else
  fail "карточка отдана как '$card_type', ожидался image/png"
fi

card_size="$(curl -fsS -o /dev/null -w '%{size_download}' "$BASE/og/work/$work_id.png" 2>/dev/null || true)"
if [ -n "$card_size" ] && [ "$card_size" -gt 5000 ] 2>/dev/null; then
  ok "карточка нетривиального размера ($card_size байт)"
else
  fail "карточка подозрительно мала: '$card_size' байт"
fi

# Файлы сборки краулеру достаются файлами, а не рендером.
asset="$(grep -o 'assets/[^"]*\.js' <<<"$human_html" | head -1 || true)"
if [ -n "$asset" ]; then
  asset_type="$(curl -fsS -o /dev/null -w '%{content_type}' -A 'Googlebot' "$BASE/$asset" 2>/dev/null || true)"
  if grep -qi 'javascript' <<<"$asset_type"; then
    ok "файл сборки ($asset) отдан краулеру как js"
  else
    fail "файл сборки отдан краулеру как '$asset_type'"
  fi
else
  fail 'в разметке для человека нет ссылки на файл сборки — SPA не отрисовалась'
fi

if [ "$failures" -gt 0 ]; then
  printf '\nпровалов: %d\n' "$failures" >&2
  exit 1
fi
printf '\nвсё сошлось\n'
