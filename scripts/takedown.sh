#!/usr/bin/env bash
# Снятие тома и снятие аппарата тома — одной командой, с замером.
#
# Зачем это нам. Правовая стойка проекта реактивная (тикет 14): ничего не
# снимаем превентивно, снимаем по первому письму. Стойка целиком держится на
# допущении, что снятие быстрое, — а операция не делалась ни разу, и сколько
# она занимает, не знал никто. Эта команда и есть измеренная готовность.
#
#   ./scripts/takedown.sh volume 145             # снять том целиком
#   ./scripts/takedown.sh apparatus 145          # снять аппарат тома
#   ./scripts/takedown.sh volume 145 --plan      # только показать, не трогать
#   ./scripts/takedown.sh apparatus 145 --prod   # против боевой читальни
#
# Что снимает «volume»: том, его полосы со сканами, главы, предметный
# указатель, служебные передние листы — и объекты хранилища под works/<id>/.
# Всё это уносит каскад схемы плюс обход по S3 внутри DELETE /api/works/{id}.
#
# Что снимает «apparatus»: главы is_apparatus с подглавами, полосы их
# диапазонов со сканами, предметный указатель тома и служебные передние листы.
# Имя говорит про аппарат, а сносит оно больше — поэтому план печатается
# поимённо и подтверждается вводом номера тома.
#
# ЧЕГО НЕ СНИМАЕТ НИ ТО, НИ ДРУГОЕ. Подстрочные и редакционные сноски и
# перевод (весь МиЭ) лежат внутри content_markdown полосы вперемешку с
# текстом: отделить их нечем, и эти слои снимаются только снятием тома
# целиком. Это ограничение, а не недоделка.
#
# Необратимо. Обратный ход — не откат, а повторная публикация тома из
# локального корпуса (tools/ocr_ingest/publish_volume.py); адреса при этом
# меняются, потому что id выдаются заново.
#
# Подробности, измеренные числа и то, куда снятие не дотягивается (копия в
# «Архиве Интернета», кэш поисковика, журнал публикации) — в шапке выше и
# в комментариях функций ниже.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Статическая читальня: список исключений следующей сборки и выкладка, которая
# снимает уже залитые сборки с этой работой. Переменные — для тестов.
STATIC_EXCLUDE="${STATIC_EXCLUDE:-$REPO_ROOT/instance/static-exclude.txt}"
STATIC_PUBLISH="${STATIC_PUBLISH:-$SCRIPT_DIR/static-publish.sh}"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Чистые функции: ни сети, ни файлов. Проверяются scripts/tests/takedown_test.sh.
# ---------------------------------------------------------------------------

# parse_target ВИД ID — проверяет пару «что снимаем» и «у чего».
#
# Номер тома берётся строго целым: ведущий слаг адреса читальни
# (/works/49-lenin-t06) сюда не годится, потому что API адресует том числом, а
# «49-lenin-t06» молча ушло бы в ParseInt как 49 не везде одинаково.
parse_target() {
  local kind="$1" id="${2:-}"
  case "$kind" in
    volume|apparatus) ;;
    *) printf 'неизвестный вид снятия: %s (ожидалось volume или apparatus)\n' "$kind"; return 1 ;;
  esac
  [[ "$id" =~ ^[1-9][0-9]*$ ]] || {
    printf 'номер тома должен быть целым числом, получено: %s\n' "${id:-<пусто>}"
    return 1
  }
  printf '%s %s\n' "$kind" "$id"
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

# plan_is_empty JSON — пуст ли план снятия аппарата.
#
# Отдельной функцией, потому что это главное правило команды: «снято ноль» на
# томе без разметки выглядит как успешное снятие и им не является. Сервер
# отвечает на такой снос 409, но команда обязана сказать это раньше — до того,
# как спросит подтверждение.
plan_is_empty() {
  local json="$1" chapters children
  chapters="$(jq -r '.chapter_count // 0' <<<"$json")"
  children="$(jq -r '(.children // []) | length' <<<"$json")"
  [[ "$chapters" == "0" && "$children" == "0" ]]
}

# index_anchor_note СТАТЕЙ_ТОМА СТАТЕЙ_СОБРАНИЯ — подпись под счётчиком статей.
#
# Счёт статей в плане идёт по ЯКОРЮ: index_concept_articles.work_id = этот том.
# А якорь проставляет только миграция 000025 (по прежней колонке понятия) —
# ни parse_index.py, ни publish_volume.py поля work_id в теле ввоза не шлют,
# и статье, заведённой ввозом, он не достаётся никогда. Значит ноль в плане
# необратимой операции означает одно из двух: указателя у собрания нет вовсе
# (снимать нечего) или указатель есть, но без якоря (снятие его не тронет, и
# оператор увидит заведомо неполный план). Различить их по самому нулю нельзя,
# поэтому подпись смотрит на второе число — статьи собрания целиком.
index_anchor_note() {
  local mine="${1:-0}" edition="${2:-0}"
  [[ "$mine" == "0" ]] || return 0
  if [[ "$edition" == "0" ]]; then
    printf 'у собрания статей указателя нет вовсе — ноль выше полон\n'
  else
    printf 'ВНИМАНИЕ: у собрания %s статей указателя, и ни одна не помечена\n' "$edition"
    printf 'этим томом (work_id) — снятие их НЕ ТРОНЕТ. Ноль выше значит\n'
    printf '«якоря на этот том нет», а не «указателя нет»: статье,\n'
    printf 'заведённой ввозом, work_id не проставляется вовсе.\n'
  fi
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

usage() {
  cat >&2 <<'USAGE'
Использование: takedown.sh {volume|apparatus} <номер тома> [ключи]

  volume      снять том целиком: полосы, сканы, главы, указатель,
              служебные передние листы, объекты хранилища
  apparatus   снять аппарат тома: главы is_apparatus с их полосами,
              предметный указатель и служебные передние листы

  --plan      только показать, что снимется; ничего не менять
  --prod      против боевой читальни (умолчание — локальный API)
  --yes       без подтверждения (для неинтерактивных прогонов)

Необратимо. Обратный ход — повторная публикация тома из локального корпуса,
и адреса при этом меняются.
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

api_ok() { [[ "$API_STATUS" =~ ^2 ]]; }

# edition_article_count ИЗДАНИЕ — сколько понятий каталога несут статью этого
# собрания. Своего счётчика в API нет, зато список понятий отдаёт edition_ids
# агрегатом; по 500 за раз это горстка запросов даже на ленинском указателе.
# Пустое издание (работа вне собрания) — ноль без единого запроса.
edition_article_count() {
  local edition="${1:-}" offset=0 n batch total=0
  [[ -n "$edition" && "$edition" != "null" ]] || { printf '0\n'; return 0; }
  while :; do
    api GET "/api/concepts?limit=500&offset=$offset" || return 1
    api_ok || return 1
    n="$(jq -r '(. // []) | length' <<<"$API_BODY")"
    [[ "$n" == "0" ]] && break
    batch="$(jq -r --arg e "$edition" \
      '[(. // [])[] | select([(.edition_ids // [])[] | tostring] | index($e))] | length' \
      <<<"$API_BODY")"
    total=$(( total + batch ))
    offset=$(( offset + n ))
    (( n < 500 )) && break
  done
  printf '%s\n' "$total"
}

# print_index_note ID СТАТЕЙ_ТОМА — подпись под счётчиком статей, если нужна.
#
# Второе число (статьи собрания) добывается ТОЛЬКО когда первое — ноль:
# на томе с якорем обход каталога незачем.
print_index_note() {
  local id="$1" mine="$2" edition note
  [[ "$mine" == "0" ]] || return 0
  api GET "/api/works/$id" || die "не удалось запросить том $id"
  api_ok || die "тома $id нет (код $API_STATUS)"
  edition="$(jq -r '.edition_id // empty' <<<"$API_BODY")"
  # Счёт идёт в подоболочке, поэтому API_BODY/API_STATUS оттуда не вернутся —
  # проверяем сам код возврата, а не их.
  local articles
  articles="$(edition_article_count "$edition")" ||
    die "не удалось пересчитать статьи собрания — план неполон, отказ"
  note="$(index_anchor_note "$mine" "$articles")"
  if [[ -n "$note" ]]; then
    while IFS= read -r line; do printf '              %s\n' "$line"; done <<<"$note"
  fi
  return 0
}

# ---------------------------------------------------------------------------
# План
# ---------------------------------------------------------------------------

# volume_plan ID — что снимет снятие тома. Собирается из уже существующих
# маршрутов: своего «плана тома» на сервере нет и заводить его незачем.
volume_plan() {
  local id="$1" work pages chapters apparatus

  api GET "/api/works/$id" || die "не удалось запросить том $id"
  api_ok || die "тома $id нет (код $API_STATUS)"
  work="$API_BODY"

  # Каждый ответ проверяется кодом, а не только первым. Это экран, по которому
  # оператор подтверждает необратимое, и непроверенный ответ печатает в нём
  # ноль вместо числа: у списочных маршрутов есть известный дефект — пустой
  # список приезжает как null, и «глав: 0» на живом томе выглядит буднично.
  api GET "/api/works/$id/page-map" || die "не удалось запросить карту полос"
  api_ok || die "карта полос тома $id не отдалась (код $API_STATUS) — план неполон, отказ"
  pages="$(jq -r '(. // []) | length' <<<"$API_BODY")"

  api GET "/api/works/$id/chapters" || die "не удалось запросить главы"
  api_ok || die "главы тома $id не отдались (код $API_STATUS) — план неполон, отказ"
  chapters="$(jq -r '[.. | objects | select(has("start_page"))] | length' <<<"$API_BODY")"

  api GET "/api/works/$id/apparatus" || die "не удалось запросить план аппарата"
  api_ok || die "состав аппарата тома $id не отдался (код $API_STATUS) — план неполон, отказ"
  apparatus="$API_BODY"

  printf 'Снятие ТОМА (необратимо)\n'
  printf '  том:        %s — %s\n' "$id" "$(jq -r '.title // "?"' <<<"$work")"
  printf '  автор:      %s\n' "$(jq -r 'if (.author // "") == "" then "—" else .author end' <<<"$work")"
  printf '  собрание:   %s, том %s\n' \
    "$(jq -r '.edition_id // "—"' <<<"$work")" "$(jq -r '.volume_number // "—"' <<<"$work")"
  printf '  полос:      %s (вместе со сканами)\n' "$pages"
  printf '  глав:       %s\n' "$chapters"
  # concept_count в API — статьи предметного указателя (index_concept_articles)
  # с work_id = этот том. В отличие от снятия АППАРАТА (Remove в
  # apparatus_repository.go, свой DELETE FROM index_concept_articles), снятие
  # ТОМА этих статей не удаляет: у index_concept_articles.work_id стоит
  # ON DELETE SET NULL (миграция 000025), а DELETE /api/works/{id} —
  # WorkRepository.Delete, голый DELETE FROM works без собственного кода на
  # статьи. Поэтому N ниже — не то, что снимется, а то, что ОСИРОТЕЕТ: у
  # стольких статей обнулится привязка к тому, а сама статья, её текст,
  # подрубрики и адреса останутся в каталоге и останутся публично читаемыми по
  # GET /api/concepts/{slug}. Снести их сейчас можно снятием аппарата (он
  # удаляет по work_id) либо вручную.
  local concepts
  concepts="$(jq -r '.concept_count // 0' <<<"$apparatus")"
  printf '  статей:     %s статей указателя ссылаются на этот том (work_id)\n' "$concepts"
  if [[ "$concepts" != "0" ]]; then
    printf '              ВНИМАНИЕ: снятие ТОМА их не удаляет — только снимает\n'
    printf '              привязку (work_id -> NULL). Статьи и их текст останутся в\n'
    printf '              каталоге и останутся читаемыми по GET /api/concepts/{slug}.\n'
    printf '              Снести их сейчас можно снятием аппарата (удаляет по\n'
    printf '              work_id) либо вручную.\n'
  fi
  print_index_note "$id" "$concepts"
  printf '  служебных:  %s\n' "$(jq -r '(.children // []) | length' <<<"$apparatus")"
  jq -r '(.children // [])[] | "    - \(.id): \(.title) (\(.pages) полос, \(.role))"' <<<"$apparatus"
  printf '  хранилище:  объекты под works/%s/ и под каждым служебным\n' "$id"
}

# apparatus_plan ID — что снимет снятие аппарата. Печатает и возвращает JSON
# плана в APPARATUS_PLAN, чтобы вызывающий проверил пустоту, не ходя дважды.
APPARATUS_PLAN=""
apparatus_plan() {
  local id="$1"
  api GET "/api/works/$id/apparatus" || die "не удалось запросить план аппарата"
  api_ok || die "тома $id нет (код $API_STATUS)"
  APPARATUS_PLAN="$API_BODY"

  printf 'Снятие АППАРАТА ТОМА (необратимо)\n'
  printf '  том:        %s — %s\n' "$id" "$(jq -r '.work_title // "?"' <<<"$APPARATUS_PLAN")"
  printf '  глав:       %s (вместе с подглавами)\n' "$(jq -r '.chapter_count // 0' <<<"$APPARATUS_PLAN")"
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
  # Аппарат не озвучивается, поэтому штатно тут ноль; ненулевое — дорожки
  # прежней раскладки или главы, размеченной аппаратом после синтеза.
  local tracks
  tracks="$(jq -r '.track_count // 0' <<<"$APPARATUS_PLAN")"
  if [[ "$tracks" != "0" ]]; then
    printf '  дорожек:    %s (синтез, задевший полосы аппарата)\n' "$tracks"
  fi
  local concepts
  concepts="$(jq -r '.concept_count // 0' <<<"$APPARATUS_PLAN")"
  printf '  статей:     %s (предметный указатель тома)\n' "$concepts"
  print_index_note "$id" "$concepts"
  printf '  служебных:  %s\n' "$(jq -r '(.children // []) | length' <<<"$APPARATUS_PLAN")"
  jq -r '(.children // [])[] | "    - \(.id): \(.title) (\(.pages) полос, \(.role))"' <<<"$APPARATUS_PLAN"
  printf '  ТЕЛО ТОМА ОСТАЁТСЯ. Сноски и перевод внутри полос тела не отделимы\n'
  printf '  и снимаются только снятием тома целиком.\n'

  # Щель, найденная замером: снятие идёт по диапазонам глав, и полосы, не
  # накрытые ни одной главой, остаются — какими бы они ни были. У ленинского
  # тома 45 это «Содержание» и колофон, то есть аппарат чистой воды. Молчать
  # об этом нельзя: невидимый остаток и есть худший исход снятия.
  local outside
  outside="$(jq -r '.pages_outside_chapters // 0' <<<"$APPARATUS_PLAN")"
  if [[ "$outside" != "0" ]]; then
    printf '  ВНИМАНИЕ: %s полос тома не накрыты НИ ОДНОЙ главой и останутся.\n' "$outside"
    printf '  Обычно это «Содержание» и выходные данные в хвосте. Если их тоже\n'
    printf '  надо снять — размечай главу или снимай том целиком.\n'
  fi
}

# exclude_line ВИД ID ДАТА — строка instance/static-exclude.txt о снятии:
# «ID» для тома, «ID apparatus» для аппарата (разбор —
# staticsite.ParseExcludeList).
exclude_line() {
  if [[ "$1" == volume ]]; then
    printf '%s  # %s: том снят по жалобе (takedown.sh volume)\n' "$2" "$3"
  else
    printf '%s apparatus  # %s: снят аппарат по жалобе (takedown.sh apparatus)\n' "$2" "$3"
  fi
}

# static_after_takedown ВИД ID — статическая читальня узнаёт о снятии с
# боевого: работа уходит в исключения следующей сборки, а сборки в бакете, где
# она лежит, снимаются сразу (static-publish.sh --withdraw). Иначе архив со
# снятым томом так и лежал бы по ссылке из справки.
static_after_takedown() {
  # Каталога экземпляра может не быть (читальня без instance/): снятие на
  # сайте к этому моменту уже прошло, падать на записи строки нельзя.
  mkdir -p "$(dirname "$STATIC_EXCLUDE")"
  exclude_line "$1" "$2" "$(date +%F)" >> "$STATIC_EXCLUDE"
  log "статическая читальня: строка дописана в $STATIC_EXCLUDE — закоммить её"
  # Инструмент выкладки — свой у каждой читальни (в платформе его нет):
  # без него выложенные сборки с этим томом снимают сами.
  if [[ ! -x "$STATIC_PUBLISH" ]]; then
    log "статическая читальня: инструмента выкладки нет ($STATIC_PUBLISH) — выложенные сборки с работой $2 снимите сами"
    return 0
  fi
  "$STATIC_PUBLISH" --withdraw "$2" --yes ||
    die "снятие на сайте прошло, а сборки статической читальни в бакете остались.
       Повтори: scripts/static-publish.sh --withdraw $2"
}

# ---------------------------------------------------------------------------

main() {
  (( $# >= 2 )) || usage
  local parsed kind id
  parsed="$(parse_target "$1" "$2")" || die "$parsed"
  read -r kind id <<<"$parsed"
  shift 2

  local plan_only=0 target=--local assume_yes=0
  while (( $# )); do
    case "$1" in
      --plan) plan_only=1; shift ;;
      --prod) target=--prod; shift ;;
      --yes)  assume_yes=1; shift ;;
      -h|--help) usage ;;
      *) die "неизвестный ключ: $1" ;;
    esac
  done

  command -v jq >/dev/null || die "нужен jq"
  load_env
  API_BASE="$(api_base "$target")"

  log "читальня: $API_BASE"
  TOKEN="$(login "$target" "$API_BASE")" || die "вход не удался"
  [[ -n "$TOKEN" ]] || die "вход не вернул токен"

  local plan_started plan_took
  plan_started="$(now_ms)"
  printf '\n'
  if [[ "$kind" == "volume" ]]; then
    volume_plan "$id"
  else
    apparatus_plan "$id"
  fi
  plan_took=$(( $(now_ms) - plan_started ))
  printf '\n'
  log "план собран за $(human_duration "$plan_took")"

  # Отказ на пустом плане — раньше подтверждения. Сервер тоже откажет (409),
  # но спросить «точно снимаем?» про набор из нуля строк значит предложить
  # оператору подтвердить бессмыслицу.
  if [[ "$kind" == "apparatus" ]] && plan_is_empty "$APPARATUS_PLAN"; then
    die "у тома $id не размечено ни одной главы аппарата и нет служебных работ — снимать нечего.
       Разметь главы (is_apparatus) или убедись, что аппарата в томе нет.
       Разметка ставится классификатором по заголовку и в корпусе неполна:
       у издания Выготского до миграции 000019 не было размечено ни одной."
  fi

  if (( plan_only )); then
    log "режим плана: ничего не изменено"
    return 0
  fi

  # Подтверждение — ввод номера тома, а не «y». Операция необратима, и цена
  # случайного Enter несоизмерима с ценой лишних четырёх нажатий.
  if (( ! assume_yes )); then
    local answer
    read -r -p "Введи номер тома ($id), чтобы снять: " answer ||
      die "подтверждения нет — отказ"
    [[ "$answer" == "$id" ]] || die "подтверждение не совпало — отказ"
  fi

  local started took
  started="$(now_ms)"
  if [[ "$kind" == "volume" ]]; then
    api DELETE "/api/works/$id" || die "запрос снятия не прошёл"
    api_ok || die "снятие не удалось (код $API_STATUS): $API_BODY"
    log "том $id снят"
  else
    api DELETE "/api/works/$id/apparatus" || die "запрос снятия не прошёл"
    api_ok || die "снятие не удалось (код $API_STATUS): $API_BODY"
    printf '%s\n' "$API_BODY" | jq -r '"снято: \(.chapter_count) глав, \(.page_count) полос, \(.concept_count) статей указателя, \((.children // []) | length) служебных работ"' >&2
  fi
  took=$(( $(now_ms) - started ))

  log "снятие заняло $(human_duration "$took") (план — $(human_duration "$plan_took"))"

  # Сверка: адрес снятого обязан отвечать 410, а не 404 и не 200. Разница не
  # косметическая — 404 поисковик перепроверяет неделями, 410 выбрасывает
  # сразу; и nginx фронта перехватывает именно 404, уводя его в SPA.
  if [[ "$kind" == "volume" ]]; then
    local code
    local probe; probe="$(crawler_path "$target" "$id")"
    code="$(curl -sS -o /dev/null -w '%{http_code}' \
      -H 'User-Agent: TelegramBot (like TwitterBot)' "$API_BASE$probe" || echo 000)"
    if [[ "$code" == "410" ]]; then
      log "сверка: адрес снятого тома отвечает 410 — верно"
    else
      log "ВНИМАНИЕ: адрес снятого тома ответил $code вместо 410 — проверь вручную"
    fi
  fi

  if [[ "$target" == "--prod" ]]; then
    static_after_takedown "$kind" "$id"
  fi

  cat >&2 <<EOF

Чего это снятие НЕ достало, и достать не может:
  - копию в «Архиве Интернета», если том туда уезжал (scripts/wayback-save.sh
    --exclude); удаление оттуда — переписка, а не команда;
  - кэш поисковика: адрес выпадет из выдачи не мгновенно (410 быстрее 404);
  - локальный корпус: том остаётся в рабочей базе, и он же — обратный ход;
  - журнал публикации tools/ocr_ingest/published/*.json: он по-прежнему
    считает том выложенным;
  - архивы статической читальни, уже скачанные читателями, и зеркала, которые
    они выложили.
EOF
}

# Источается тестом — тогда main не запускается.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
