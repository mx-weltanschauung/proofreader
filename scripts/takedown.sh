#!/usr/bin/env bash
# Снятие по жалобе — ЗАКРЫТИЕМ, а не удалением. Одной командой, с причиной.
#
# Правовая стойка проекта реактивная (тикет 14): ничего не снимаем
# превентивно, снимаем по первому письму. Раньше снятие сносило данные
# (DELETE) и было необратимо; теперь оно закрывает: закрытое видят только
# editor и administrator, анониму и читателю его нет ни в одном канале (API —
# 404, /seo и .md — 410), а открыть обратно можно одной командой.
#
#   ./scripts/takedown.sh volume 145 --reason "письмо правообладателя от 12.10"
#   ./scripts/takedown.sh apparatus 145 --reason "..."     # аппарат тома
#   ./scripts/takedown.sh chapter 145/9923 --reason "..."  # одна глава
#   ./scripts/takedown.sh volume 145 --plan                # только показать
#   ./scripts/takedown.sh volume 145 --reason "..." --open # вернуть обратно
#   ./scripts/takedown.sh chapter 145/9923 --reason "..." --prod
#
# Закрывать и открывать может только администратор; причина обязательна в
# обе стороны и попадает в журнал закрытий (GET /api/admin/closures).
#
# Что закрывает «apparatus»: корни глав is_apparatus (вместе с подглавами) и
# служебные (front_matter) работы тома. Тело тома остаётся открытым.
#
# Что закрывает «chapter»: главу с подглавами. Полосы закрытой главы
# закрыты, кроме тех, что накрывает и открытая глава: такие полосы остаются
# видны через соседа (план печатает их и соседей). Родитель закрытой подглавы
# теряет её вместе с полосами — так и задумано.
#
# ЧЕГО НЕ ДОСТАЁТ. Копия в «Архиве Интернета», кэш поисковика, уже
# скачанные читателями файлы и архивы — подробности в docs/TAKEDOWN.md.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Статическая читальня: выкладка, которая снимает уже залитые сборки с этой
# работой. Переменная — для тестов. Списка исключений скрипт не пишет.
STATIC_PUBLISH="${STATIC_PUBLISH:-$SCRIPT_DIR/static-publish.sh}"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Чистые функции: ни сети, ни файлов. Проверяются scripts/tests/takedown_test.sh.
# ---------------------------------------------------------------------------

# parse_target ВИД ЦЕЛЬ — проверяет пару «что закрываем» и «у чего».
#
# Том — целое число: ведущий слаг адреса читальни (/works/49-lenin-t06) сюда
# не годится, API адресует том числом. Глава — пара «том/глава», потому что
# маршрут главы адресуется парой. Печатает «вид том [глава]».
parse_target() {
  local kind="$1" id="${2:-}"
  case "$kind" in
    volume|apparatus)
      [[ "$id" =~ ^[1-9][0-9]*$ ]] || {
        printf 'номер тома должен быть целым числом, получено: %s\n' "${id:-<пусто>}"
        return 1
      }
      printf '%s %s\n' "$kind" "$id" ;;
    chapter)
      [[ "$id" =~ ^([1-9][0-9]*)/([1-9][0-9]*)$ ]] || {
        printf 'глава адресуется парой том/глава (12/345), получено: %s\n' "${id:-<пусто>}"
        return 1
      }
      printf 'chapter %s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" ;;
    *) printf 'неизвестный вид: %s (ожидалось volume, apparatus или chapter)\n' "$kind"; return 1 ;;
  esac
}

# require_reason ТЕКСТ — причина обязательна и в закрытии, и в открытии.
# Пустая или из одних пробелов — отказ (сервер ответил бы 400 «Укажите
# причину», но проверяем до входа).
require_reason() {
  local r="${1:-}"
  r="${r//[[:space:]]/}"
  [[ -n "$r" ]] || { printf 'Укажите причину: --reason "письмо, от кого, о чём"\n' >&2; return 1; }
}

# closed_body true|false ПРИЧИНА — тело запроса. Через jq --arg, без
# подстановки в строку: кавычки и переводы строк в причине обязаны уцелеть.
closed_body() {
  jq -n --argjson c "$1" --arg r "$2" '{closed:$c, reason:$r}'
}

# probe_ok ОЖИДАНИЕ КОД — сходится ли код анонимной пробы. Ожидание: 410, 404
# или open (вернулось: не 404 и не 410).
probe_ok() {
  local want="$1" code="$2"
  if [[ "$want" == open ]]; then
    [[ "$code" != 404 && "$code" != 410 && "$code" != 000 ]]
  else
    [[ "$code" == "$want" ]]
  fi
}

# human_duration МИЛЛИСЕКУНДЫ — «0.5 с», «4 мин 07 с».
#
# Миллисекунды, а не секунды: замер показал, что снятие тома в 488 полос со
# всеми сканами занимает полсекунды, и целочисленный счёт печатал «0 с» — что
# читается как сломанный секундомер, а не как быстрая операция. Ради этого
# числа команда и написана, и оно обязано быть видно.
human_duration() {
  local ms="$1" s=$(( ($1 + 500) / 1000 ))
  if (( ms < 10000 )); then
    # Округление, а не отбрасывание: 497 мс это «0.5 с», а не «0.4 с».
    local tenths=$(( (ms + 50) / 100 ))
    printf '%d.%01d с\n' "$((tenths / 10))" "$((tenths % 10))"
  elif (( s < 60 )); then
    printf '%d с\n' "$s"
  else
    printf '%d мин %02d с\n' "$((s / 60))" "$((s % 60))"
  fi
}

# now_ms — монотонный-достаточно секундомер. date +%s%N есть и в coreutils, и
# в busybox; на macOS его нет, но остальные скрипты набора и так линуксовые.
now_ms() { echo $(( $(date +%s%N) / 1000000 )); }

# plan_is_empty JSON — пуст ли план закрытия аппарата.
#
# Отдельной функцией, потому что это главное правило команды: «снято ноль» на
# томе без разметки выглядит как успешное закрытие и им не является. Сервер
# отвечает на такой снос 409, но команда обязана сказать это раньше — до того,
# как спросит подтверждение.
plan_is_empty() {
  local json="$1" chapters children
  chapters="$(jq -r '.chapter_count // 0' <<<"$json")"
  children="$(jq -r '(.children // []) | length' <<<"$json")"
  [[ "$chapters" == "0" && "$children" == "0" ]]
}

# api_base --prod|--local — адрес API.
api_base() {
  if [[ "${1:-}" == "--prod" ]]; then
    [[ -n "${PROD_API_URL:-}" ]] || die "--prod: задайте PROD_API_URL — адрес живой читальни"
    printf '%s\n' "$PROD_API_URL"
  else
    printf '%s\n' "${API_URL:-http://localhost:8080}"
  fi
}

# crawler_path --prod|--local ID — по какому адресу спрашивать страницу
# краулера, чтобы сверить 410.
#
# Разница не косметическая, и на ней сверка уже была сломана. Префикс /seo
# дописывает nginx фронта (карта $is_crawler в frontend/nginx.conf), а не
# бэкенд: на боевом краулер просит /works/N и получает отрисованную страницу,
# а голый бэкенд той же формы адреса не знает вовсе и честно отвечает 404 —
# то есть сверка против локального стека всегда ругалась бы впустую.
crawler_path() {
  if [[ "${1:-}" == "--prod" ]]; then
    printf '/works/%s\n' "$2"
  else
    printf '/seo/works/%s\n' "$2"
  fi
}

# crawler_chapter_path --prod|--local ТОМ ГЛАВА — то же для страницы главы.
crawler_chapter_path() {
  if [[ "${1:-}" == "--prod" ]]; then
    printf '/works/%s/chapters/%s\n' "$2" "$3"
  else
    printf '/seo/works/%s/chapters/%s\n' "$2" "$3"
  fi
}

usage() {
  cat >&2 <<'USAGE'
Использование: takedown.sh {volume|apparatus} <том> ключи
               takedown.sh chapter <том>/<глава> ключи

  volume      закрыть том целиком
  apparatus   закрыть аппарат тома: главы is_apparatus и служебные работы
  chapter     закрыть главу (с подглавами); адрес — пара 12/345

  --reason T  причина (обязательна, и при открытии тоже)
  --open      обратный ход: открыть то же самое
  --plan      только показать, что закроется; ничего не менять
  --prod      против боевой читальни (умолчание — локальный API)
  --yes       без подтверждения (для неинтерактивных прогонов)

Закрытое видят только editor и administrator; администратор открывает
обратно этой же командой. Журнал — GET /api/admin/closures.
USAGE
  exit 2
}

# ---------------------------------------------------------------------------
# Обвязка API
# ---------------------------------------------------------------------------

load_env() {
  if [[ -f "$REPO_ROOT/.env" ]]; then
    set -a; # shellcheck disable=SC1091
    source "$REPO_ROOT/.env"; set +a
  fi
  # Боевые реквизиты живут там же, где у публикатора тома: это один и тот же
  # конвейер, и искать их будут в одном месте.
  if [[ -f "$REPO_ROOT/tools/ocr_ingest/.env" ]]; then
    set -a; # shellcheck disable=SC1091
    source "$REPO_ROOT/tools/ocr_ingest/.env"; set +a
  fi
}

# login --prod|--local БАЗА — токен администратора.
#
# Реквизиты выбираются по цели, а НЕ сравнением адреса с PROD_API_URL: при
# незаполненном PROD_API_URL сравнение не совпадало бы ни с чем, и `--prod`
# пошёл бы на боевую читальню с локальным admin/admin. Боевые живут в
# tools/ocr_ingest/.env — там же, где их держит публикатор тома: это один
# конвейер, и искать их будут в одном месте.
login() {
  local target="$1" base="$2" email password body
  if [[ "$target" == "--prod" ]]; then
    email="${PROD_ADMIN_EMAIL:-}"; password="${PROD_ADMIN_PASSWORD:-}"
    [[ -n "$email" && -n "$password" ]] ||
      die "нет PROD_ADMIN_EMAIL/PROD_ADMIN_PASSWORD (tools/ocr_ingest/.env)"
  else
    email="${ADMIN_EMAIL:-admin@proofreader.local}"
    password="${ADMIN_PASSWORD:-admin}"
  fi

  body="$(jq -n --arg e "$email" --arg p "$password" '{email:$e,password:$p}')"
  curl -fsS -X POST "$base/api/auth/login" \
    -H 'Content-Type: application/json' -d "$body" |
    jq -r '.token // empty'
}

# api МЕТОД ПУТЬ — запрос с токеном. Тело в API_BODY, код в API_STATUS.
#
# Тело НЕ печатается в stdout намеренно: вызов вида `body="$(api ...)"` уходит
# в подоболочку, откуда присвоенный код возврата не вернётся. На этом уже была
# сломана целиком обвязка yadisk-sync.sh — повторять незачем.
API_BODY=""
API_STATUS=""
api() {
  local method="$1" path="$2" out
  out="$(curl -sS -X "$method" "$API_BASE$path" \
    -H "Authorization: Bearer $TOKEN" \
    -w $'\n%{http_code}')" || return 1
  API_STATUS="${out##*$'\n'}"
  API_BODY="${out%$'\n'*}"
}

# api_put ПУТЬ ТЕЛО — PUT с JSON-телом; результат — как у api.
api_put() {
  local path="$1" body="$2" out
  out="$(curl -sS -X PUT "$API_BASE$path" \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -d "$body" -w $'\n%{http_code}')" || return 1
  API_STATUS="${out##*$'\n'}"
  API_BODY="${out%$'\n'*}"
}

api_ok() { [[ "$API_STATUS" =~ ^2 ]]; }

# ---------------------------------------------------------------------------
# План
# ---------------------------------------------------------------------------

# volume_plan ID — что закроет закрытие тома. Собирается из существующих
# маршрутов: своего «плана тома» на сервере нет и заводить его незачем.
volume_plan() {
  local id="$1" work pages chapters apparatus

  api GET "/api/works/$id" || die "не удалось запросить том $id"
  api_ok || die "тома $id нет (код $API_STATUS)"
  work="$API_BODY"

  api GET "/api/works/$id/page-map" || die "не удалось запросить карту полос"
  api_ok || die "карта полос тома $id не отдалась (код $API_STATUS) — план неполон, отказ"
  pages="$(jq -r '(. // []) | length' <<<"$API_BODY")"

  api GET "/api/works/$id/chapters" || die "не удалось запросить главы"
  api_ok || die "главы тома $id не отдались (код $API_STATUS) — план неполон, отказ"
  chapters="$(jq -r '[.. | objects | select(has("start_page"))] | length' <<<"$API_BODY")"

  api GET "/api/works/$id/apparatus" || die "не удалось запросить состав аппарата"
  api_ok || die "состав аппарата тома $id не отдался (код $API_STATUS) — план неполон, отказ"
  apparatus="$API_BODY"

  printf '%s ТОМА\n' "$ACTION_TITLE"
  printf '  том:        %s — %s\n' "$id" "$(jq -r '.title // "?"' <<<"$work")"
  printf '  автор:      %s\n' "$(jq -r 'if (.author // "") == "" then "—" else .author end' <<<"$work")"
  printf '  собрание:   %s, том %s\n' \
    "$(jq -r '.edition_id // "—"' <<<"$work")" "$(jq -r '.volume_number // "—"' <<<"$work")"
  printf '  полос:      %s\n' "$pages"
  printf '  глав:       %s\n' "$chapters"
  printf '  служебных:  %s (закрываются вместе с томом)\n' "$(jq -r '(.children // []) | length' <<<"$apparatus")"
  jq -r '(.children // [])[] | "    - \(.id): \(.title) (\(.pages) полос, \(.role))"' <<<"$apparatus"
  printf '  Данные не удаляются: закрытое остаётся в базе и в хранилище.\n'
}

# chapter_plan ТОМ ГЛАВА — план из closed-plan: сколько глав и полос закроется,
# какие полосы общие с открытыми главами.
CHAPTER_PLAN=""
chapter_plan() {
  local work="$1" chapter="$2"
  api GET "/api/works/$work/chapters/$chapter/closed-plan" || die "не удалось запросить план главы"
  api_ok || die "план главы $work/$chapter не отдался (код $API_STATUS): $API_BODY"
  CHAPTER_PLAN="$API_BODY"
  printf '%s ГЛАВЫ\n' "$ACTION_TITLE"
  printf '  глава:      %s в томе %s\n' "$chapter" "$work"
  printf '  глав:       %s (с подглавами)\n' "$(jq -r '.chapters // 0' <<<"$CHAPTER_PLAN")"
  printf '  полос:      %s\n' "$(jq -r '.pages // 0' <<<"$CHAPTER_PLAN")"
  local shared
  shared="$(jq -r '(.shared // []) | length' <<<"$CHAPTER_PLAN")"
  if [[ "$shared" != "0" ]]; then
    printf '  общих полос: %s — их накрывает и открытая глава, поэтому они\n' "$shared"
    printf '              остаются видны через соседа:\n'
    jq -r '(.shared // [])[] | "    - полоса \(.page_number): \((.neighbors // []) | join(", "))"' <<<"$CHAPTER_PLAN"
  fi
  printf '  Родительские главы в списке соседей не названы: родитель теряет\n'
  printf '  закрытую подглаву вместе с её полосами — так и задумано.\n'
}

# apparatus_plan ID — что закроет закрытие аппарата. Печатает и возвращает JSON
# плана в APPARATUS_PLAN, чтобы вызывающий проверил пустоту, не ходя дважды.
APPARATUS_PLAN=""
apparatus_plan() {
  local id="$1"
  api GET "/api/works/$id/apparatus" || die "не удалось запросить план аппарата"
  api_ok || die "тома $id нет (код $API_STATUS)"
  APPARATUS_PLAN="$API_BODY"

  printf '%s АППАРАТА ТОМА\n' "$ACTION_TITLE"
  printf '  том:        %s — %s\n' "$id" "$(jq -r '.work_title // "?"' <<<"$APPARATUS_PLAN")"
  printf '  глав:       %s (корни и подглавы; закрываются корни)\n' "$(jq -r '.chapter_count // 0' <<<"$APPARATUS_PLAN")"
  # Глава с parent_title сидит ВНУТРИ работы, не помеченной аппаратом, — почти
  # наверняка это авторское приложение, а не аппарат издания (у Маркса так
  # помечены сто полос его собственного текста). Снос её всё равно возьмёт,
  # поэтому строка печатается с восклицанием, а не наравне с остальными.
  jq -r '(.chapters // [])[]
         | (if (.parent_title // "") == "" then "    - " else "    ! " end)
         + "\(.id): \(.title) [\(.start_page)—\(.end_page)]"
         + (if .subchapters > 0 then " + \(.subchapters) подглав" else "" end)
         + (if (.parent_title // "") == "" then "" else "  ← ВНУТРИ «\(.parent_title)», проверь: похоже на авторское приложение" end)' <<<"$APPARATUS_PLAN"
  printf '  полос:      %s (вместе со сканами)\n' "$(jq -r '.page_count // 0' <<<"$APPARATUS_PLAN")"
  printf '  служебных:  %s\n' "$(jq -r '(.children // []) | length' <<<"$APPARATUS_PLAN")"
  jq -r '(.children // [])[] | "    - \(.id): \(.title) (\(.pages) полос, \(.role))"' <<<"$APPARATUS_PLAN"
  printf '  ТЕЛО ТОМА ОСТАЁТСЯ. Сноски и перевод внутри полос тела не отделимы\n'
  printf '  и закрываются только закрытием тома целиком.\n'

  # Щель, найденная замером: закрытие идёт по главам, и полосы, не
  # накрытые ни одной главой, остаются — какими бы они ни были. У ленинского
  # тома 45 это «Содержание» и колофон, то есть аппарат чистой воды. Молчать
  # об этом нельзя: невидимый остаток и есть худший исход закрытия.
  local outside
  outside="$(jq -r '.pages_outside_chapters // 0' <<<"$APPARATUS_PLAN")"
  if [[ "$outside" != "0" ]]; then
    printf '  ВНИМАНИЕ: %s полос тома не накрыты НИ ОДНОЙ главой и останутся.\n' "$outside"
    printf '  Обычно это «Содержание» и выходные данные в хвосте. Если их тоже\n'
    printf '  надо закрыть — размечай главу или закрывай том целиком.\n'
  fi
}

# static_after_takedown ID — сборки статической читальни, уже выложенные в
# бакет и содержащие работу, снимаются сразу (static-publish.sh --withdraw).
# Следующую сборку ведёт сама сборка: она узнаёт закрытое с боевого (карты
# полос анонимно, служебные работы с 404), строки в списке исключений
# больше не пишутся. Инструмент выкладки — свой у каждой читальни (в
# платформе его нет): без него выложенные сборки снимают сами.
static_after_takedown() {
  if [[ ! -x "$STATIC_PUBLISH" ]]; then
    log "статическая читальня: инструмента выкладки нет ($STATIC_PUBLISH) — выложенные сборки с работой $1 снимите сами"
    return 0
  fi
  "$STATIC_PUBLISH" --withdraw "$1" --yes ||
    die "закрытие на сайте прошло, а сборки статической читальни в бакете остались.
       Повтори: scripts/static-publish.sh --withdraw $1"
}

# ---------------------------------------------------------------------------

main() {
  (( $# >= 2 )) || usage
  local parsed kind id chapter=""
  parsed="$(parse_target "$1" "$2")" || die "$parsed"
  read -r kind id chapter <<<"$parsed"
  shift 2

  local plan_only=0 target=--local assume_yes=0 reopen=0 reason=""
  while (( $# )); do
    case "$1" in
      --plan) plan_only=1; shift ;;
      --prod) target=--prod; shift ;;
      --yes)  assume_yes=1; shift ;;
      --open) reopen=1; shift ;;
      --reason)
        (( $# >= 2 )) || die "--reason требует текст"
        reason="$2"; shift 2 ;;
      -h|--help) usage ;;
      *) die "неизвестный ключ: $1" ;;
    esac
  done

  command -v jq >/dev/null || die "нужен jq"
  # Причина — до входа и до сети. План ничего не меняет и причины не просит.
  if (( ! plan_only )); then
    require_reason "$reason" || exit 1
  fi
  load_env
  API_BASE="$(api_base "$target")"

  local closed=true want_seo=410 want_api=404
  ACTION_TITLE="Закрытие"
  if (( reopen )); then
    closed=false; want_seo=open; want_api=open; ACTION_TITLE="Открытие"
  fi

  log "читальня: $API_BASE"
  TOKEN="$(login "$target" "$API_BASE")" || die "вход не удался"
  [[ -n "$TOKEN" ]] || die "вход не вернул токен"

  local plan_started plan_took
  plan_started="$(now_ms)"
  printf '\n'
  case "$kind" in
    volume) volume_plan "$id" ;;
    apparatus) apparatus_plan "$id" ;;
    chapter) chapter_plan "$id" "$chapter" ;;
  esac
  plan_took=$(( $(now_ms) - plan_started ))
  printf '\n'
  log "план собран за $(human_duration "$plan_took")"

  # Отказ на пустом плане — раньше подтверждения: «закрыто ноль» не должно
  # выглядеть как закрытие.
  if [[ "$kind" == "apparatus" ]] && plan_is_empty "$APPARATUS_PLAN"; then
    die "у тома $id не размечено ни одной главы аппарата и нет служебных работ — закрывать нечего.
       Разметь главы (is_apparatus) или убедись, что аппарата в томе нет."
  fi

  if (( plan_only )); then
    log "режим плана: ничего не изменено"
    return 0
  fi

  # Подтверждение — ввод номера тома (для главы — пары), а не «y».
  local label="$id"
  [[ "$kind" == chapter ]] && label="$id/$chapter"
  if (( ! assume_yes )); then
    local answer
    read -r -p "Введи $label, чтобы подтвердить: " answer ||
      die "подтверждения нет — отказ"
    [[ "$answer" == "$label" ]] || die "подтверждение не совпало — отказ"
  fi

  local body started took r c
  body="$(closed_body "$closed" "$reason")"
  started="$(now_ms)"
  # put_closed ПУТЬ — один PUT .../closed с телом; любой не-2xx — отказ.
  put_closed() {
    api_put "$1" "$body" || die "запрос не прошёл: $1"
    api_ok || die "не удалось (код $API_STATUS) на $1: $API_BODY"
  }
  local roots=() children=()
  case "$kind" in
    volume)
      put_closed "/api/works/$id/closed"
      log "том $id: closed=$closed" ;;
    chapter)
      put_closed "/api/works/$id/chapters/$chapter/closed"
      log "глава $id/$chapter: closed=$closed" ;;
    apparatus)
      mapfile -t roots < <(jq -r '(.chapters // [])[] | .id' <<<"$APPARATUS_PLAN")
      mapfile -t children < <(jq -r '(.children // [])[] | .id' <<<"$APPARATUS_PLAN")
      for r in "${roots[@]}"; do
        [[ -n "$r" ]] || continue
        put_closed "/api/works/$id/chapters/$r/closed"
        log "глава аппарата $r: closed=$closed"
      done
      for c in "${children[@]}"; do
        [[ -n "$c" ]] || continue
        put_closed "/api/works/$c/closed"
        log "служебная работа $c: closed=$closed"
      done ;;
  esac
  took=$(( $(now_ms) - started ))
  log "заняло $(human_duration "$took") (план — $(human_duration "$plan_took"))"

  # Сверка анонимно. Закрытое: /seo отвечает 410 (не 404 — 404 поисковик
  # перепроверяет неделями, и nginx фронта уводит его в SPA), служебные
  # работы аппарата в API — 404. Открытое: не 404 и не 410.
  probe() { # ПОДПИСЬ ОЖИДАНИЕ URL
    local code
    code="$(curl -sS -o /dev/null -w '%{http_code}' \
      -H 'User-Agent: TelegramBot (like TwitterBot)' "$3" || echo 000)"
    if probe_ok "$2" "$code"; then
      log "сверка: $1 — $code, верно"
    else
      log "ВНИМАНИЕ: $1 ответил $code (ожидалось: $2) — проверь вручную"
    fi
  }
  case "$kind" in
    volume)
      probe "страница тома $id" "$want_seo" "$API_BASE$(crawler_path "$target" "$id")" ;;
    chapter)
      probe "страница главы $id/$chapter" "$want_seo" "$API_BASE$(crawler_chapter_path "$target" "$id" "$chapter")" ;;
    apparatus)
      for r in "${roots[@]}"; do
        [[ -n "$r" ]] || continue
        probe "глава аппарата $r" "$want_seo" "$API_BASE$(crawler_chapter_path "$target" "$id" "$r")"
      done
      for c in "${children[@]}"; do
        [[ -n "$c" ]] || continue
        probe "служебная работа $c (API анониму)" "$want_api" "$API_BASE/api/works/$c"
      done ;;
  esac

  if [[ "$target" == "--prod" && "$closed" == true ]]; then
    static_after_takedown "$id"
  fi

  cat >&2 <<'TAIL'

Журнал закрытий: GET /api/admin/closures (экран /admin/closures).
Чего закрытие НЕ достало, и достать не может:
  - копию в «Архиве Интернета», если том туда уезжал (scripts/wayback-save.sh
    --exclude); удаление оттуда — переписка, а не команда;
  - кэш поисковика: адрес выпадет из выдачи не мгновенно (410 быстрее 404);
  - файлы и архивы статической читальни, уже скачанные читателями;
  - журнал публикации tools/ocr_ingest/published/*.json.
Обратный ход: та же команда с --open и причиной.
TAIL
}

# Источается тестом — тогда main не запускается.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
