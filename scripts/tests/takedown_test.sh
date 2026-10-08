#!/usr/bin/env bash
# Тесты чистых функций и HTTP-обвязки takedown.sh. Сети не нужно, curl
# подставной; docker не нужен.
#
# Проверяется то, ценой чего команда необратима: разбор цели (номер тома —
# целое, а не ведущий слаг адреса), опознание пустого плана (главное правило —
# «снято ноль» не успех) и то, что код ответа доходит до вызывающего.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../takedown.sh
source "$TESTS_DIR/../takedown.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

TOKEN="тестовый-токен"
API_BASE="http://пример"

STUB_BODY=""
STUB_CODE=""

# curl подменяется целиком. Обвязка api зовёт его с -w '\n%{http_code}' и
# ждёт тело плюс строку кода; login зовёт БЕЗ -w и отдаёт вывод прямо в jq,
# которому приклеенный код ломает разбор. Первая версия заглушки приклеивала
# код всегда — тест входа при этом оставался зелёным, потому что assert видел
# только первую строку вывода jq, а сообщение об ошибке уходило в stderr. То
# есть регрессию в добыче токена он бы не поймал. Заглушка смотрит на -w.
curl() {
  local with_status=0 a
  for a in "$@"; do [[ "$a" == -w ]] && with_status=1; done
  if (( with_status )); then
    printf '%s\n%s' "$STUB_BODY" "$STUB_CODE"
  else
    printf '%s' "$STUB_BODY"
  fi
}

assert_eq() {
  local expected="$1" got="$2" what="${3:-значение}"
  [[ "$got" == "$expected" ]] || fail "$what расходится
    ожидалось: $expected
    получено:  $got"
}

# --- разбор цели -----------------------------------------------------------

test_parse_target_accepts_both_kinds() {
  assert_eq "volume 145" "$(parse_target volume 145)" "разбор снятия тома"
  assert_eq "apparatus 45" "$(parse_target apparatus 45)" "разбор снятия аппарата"
}

test_parse_target_rejects_unknown_kind() {
  local rc=0 out
  out="$(parse_target scan 145)" || rc=$?
  (( rc != 0 )) || fail "неизвестный вид снятия принят"
  [[ "$out" == *"scan"* ]] || fail "в отказе не назван вид: $out"
}

# Адреса читальни несут слаг (/works/49-lenin-t06), и рука тянется вставить
# его целиком. API адресует том числом, и «49-lenin-t06» обязано быть отказом,
# а не молчаливым 49: снять не тот том необратимо.
test_parse_target_rejects_url_slug() {
  local rc=0
  parse_target volume 49-lenin-t06 >/dev/null || rc=$?
  (( rc != 0 )) || fail "слаг адреса принят как номер тома"
}

test_parse_target_rejects_nonsense_ids() {
  local bad rc
  for bad in "" 0 -1 0145 "1 2" "1;rm" "сорок"; do
    rc=0
    parse_target volume "$bad" >/dev/null || rc=$?
    (( rc != 0 )) || fail "номер тома «$bad» принят"
  done
}

# --- пустой план -----------------------------------------------------------

# Главное правило команды. Разметка аппарата ставится классификатором по
# заголовку и в корпусе неполна; у издания Выготского до миграции 000019 не
# было размечено ни одной главы из 169 — и снятие «успешно» унесло бы ноль.
test_plan_is_empty_on_unmarked_volume() {
  local json='{"work_id":145,"chapter_count":0,"page_count":0,"concept_count":0,"children":[]}'
  plan_is_empty "$json" || fail "план без глав и без служебных работ не опознан пустым"
}

test_plan_is_not_empty_when_chapters_are_marked() {
  local json='{"chapter_count":5,"page_count":27,"children":[]}'
  plan_is_empty "$json" && fail "план с пятью главами опознан пустым"
  return 0
}

# Служебные передние листы снимаются той же командой, поэтому том без единой
# главы аппарата, но со служебным ребёнком, пустым планом не является.
test_plan_is_not_empty_when_only_children_remain() {
  local json='{"chapter_count":0,"children":[{"id":46,"title":"Передние листы","pages":4}]}'
  plan_is_empty "$json" && fail "план со служебной работой опознан пустым"
  return 0
}

test_index_anchor_note_stays_quiet_when_articles_are_counted() {
  # Счётчик непустой — он говорит сам за себя, подпись не нужна.
  assert_eq "" "$(index_anchor_note 12 300)" "подпись при непустом счёте"
}

test_index_anchor_note_distinguishes_no_index_from_no_anchor() {
  # Ноль статей тома при НУЛЕ статей собрания — указателя действительно нет.
  local none anchorless
  none="$(index_anchor_note 0 0)"
  anchorless="$(index_anchor_note 0 2819)"
  case "$none" in
    *"у собрания статей указателя нет"*) ;;
    *) fail "подпись про отсутствующий указатель не названа: $none" ;;
  esac
  # Ноль статей тома при непустом указателе собрания — снятие их не тронет, и
  # оператор обязан это увидеть ДО необратимой операции.
  case "$anchorless" in
    *2819*"work_id"*) ;;
    *) fail "подпись про отсутствующий якорь не названа: $anchorless" ;;
  esac
  [[ "$none" != "$anchorless" ]] || fail "две разные причины нуля напечатаны одинаково"
}

test_plan_is_empty_survives_missing_keys() {
  plan_is_empty '{}' || fail "ответ без ключей не опознан пустым"
}

# --- обвязка API -----------------------------------------------------------

test_status_and_body_reach_the_caller() {
  STUB_BODY='{"message":"Том не найден"}' STUB_CODE=404
  api GET /api/works/999/apparatus
  assert_eq 404 "$API_STATUS" "код ответа"
  assert_eq '{"message":"Том не найден"}' "$API_BODY" "тело ответа"
}

test_multiline_body_does_not_eat_the_status() {
  STUB_BODY=$'{\n  "chapter_count": 5\n}' STUB_CODE=200
  api GET /api/works/45/apparatus
  assert_eq 200 "$API_STATUS" "код при многострочном теле"
  assert_eq 5 "$(jq -r .chapter_count <<<"$API_BODY")" "разбор многострочного тела"
}

test_api_ok_follows_the_status() {
  STUB_BODY='{}' STUB_CODE=200
  api GET /api/works/45/apparatus
  api_ok || fail "успешный ответ не принят api_ok"
  STUB_BODY='{}' STUB_CODE=409
  api DELETE /api/works/45/apparatus
  api_ok && fail "409 принят api_ok за успех"
  return 0
}

# --- вывод длительности ----------------------------------------------------

# Счёт в миллисекундах, а не в секундах: замер показал, что снятие тома в 488
# полос занимает полсекунды, и целочисленные секунды печатали «0 с» — вид
# сломанного секундомера вместо числа, ради которого команда и написана.
test_human_duration_reads_as_time() {
  assert_eq "0.5 с" "$(human_duration 497)" "полсекунды"
  assert_eq "0.0 с" "$(human_duration 12)" "двенадцать миллисекунд"
  assert_eq "9.9 с" "$(human_duration 9940)" "почти десять секунд"
  assert_eq "0.1 с" "$(human_duration 51)" "округление вверх, а не отбрасывание"
  assert_eq "47 с" "$(human_duration 47000)" "секунды"
  assert_eq "1 мин 00 с" "$(human_duration 60000)" "ровно минута"
  assert_eq "4 мин 07 с" "$(human_duration 247000)" "минуты с секундами"
}

# --- выбор читальни --------------------------------------------------------

# Реквизиты выбираются по цели, а не сравнением адреса с PROD_API_URL: при
# незаполненном PROD_API_URL сравнение не совпало бы ни с чем, и `--prod` пошёл
# бы на боевую читальню с локальным admin/admin.
test_prod_login_refuses_without_prod_credentials() {
  local rc=0 out
  out="$( ( unset PROD_ADMIN_EMAIL PROD_ADMIN_PASSWORD
            unset PROD_API_URL
            login --prod https://lib.example ) 2>&1 )" || rc=$?
  (( rc != 0 )) || fail "вход на боевой без боевых реквизитов не отказал"
  [[ "$out" == *PROD_ADMIN_EMAIL* ]] || fail "в отказе не названы нужные переменные: $out"
}

test_local_login_uses_local_credentials() {
  STUB_BODY='{"token":"локальный"}' STUB_CODE=200
  local got
  got="$( ADMIN_EMAIL=a@b ADMIN_PASSWORD=pw login --local http://localhost:8080 )"
  assert_eq "локальный" "$got" "токен локального входа"
}


test_api_base_defaults_to_local() {
  ( unset API_URL; assert_eq "http://localhost:8080" "$(api_base --local)" "локальный адрес" )
}

# Префикс /seo дописывает nginx фронта, а не бэкенд. Сверка 410 против голого
# локального бэкенда по адресу /works/N всегда получала бы 404 — формы этого
# адреса он не знает, — и сверка ругалась бы впустую на исправном снятии.
# Ровно так она и была написана сначала, и замер это вскрыл.
test_crawler_path_carries_seo_prefix_only_locally() {
  assert_eq "/seo/works/45" "$(crawler_path --local 45)" "локальный адрес страницы краулера"
  assert_eq "/works/45" "$(crawler_path --prod 45)" "боевой адрес страницы краулера"
}

# Без адреса живой читальни --prod не угадывает: умолчанием был бы чужой или
# выдуманный сайт, и снятие ушло бы не туда.
test_api_base_prod_refuses_without_url() {
  local rc=0
  ( unset PROD_API_URL; api_base --prod ) >/dev/null 2>&1 || rc=$?
  (( rc != 0 )) || fail "api_base --prod без PROD_API_URL не отказал"
}

test_api_base_prod_uses_prod_url() {
  ( PROD_API_URL="https://lib.example"; assert_eq "https://lib.example" "$(api_base --prod)" "боевой адрес" )
}

# --- статическая читальня --------------------------------------------------

test_exclude_line_forms() {
  assert_eq "145  # 2026-10-07: том снят по жалобе (takedown.sh volume)" \
    "$(exclude_line volume 145 2026-10-07)" "строка тома"
  assert_eq "145 apparatus  # 2026-10-07: снят аппарат по жалобе (takedown.sh apparatus)" \
    "$(exclude_line apparatus 145 2026-10-07)" "строка аппарата"
}

test_static_after_takedown_appends_and_withdraws() {
  STATIC_EXCLUDE="$TMPROOT/exclude.txt"
  printf '# шапка\n' > "$STATIC_EXCLUDE"
  STATIC_PUBLISH="$TMPROOT/publish"
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" >> "%s/calls"\n' "$TMPROOT" > "$STATIC_PUBLISH"
  chmod +x "$STATIC_PUBLISH"
  ( static_after_takedown apparatus 145 ) 2>/dev/null || fail "снятие в бакете упало"
  grep -qE '^145 apparatus  # [0-9-]+: снят аппарат' "$STATIC_EXCLUDE" || fail "строка аппарата не дописана: $(cat "$STATIC_EXCLUDE")"
  head -1 "$STATIC_EXCLUDE" | grep -qx '# шапка' || fail "шапка списка затёрта"
  assert_eq "--withdraw 145 --yes" "$(cat "$TMPROOT/calls")" "вызов выкладки"
}

test_static_after_takedown_fails_loudly() {
  STATIC_EXCLUDE="$TMPROOT/exclude.txt"
  STATIC_PUBLISH="$TMPROOT/publish"
  printf '#!/usr/bin/env bash\nexit 1\n' > "$STATIC_PUBLISH"
  chmod +x "$STATIC_PUBLISH"
  local err
  err="$( ( static_after_takedown volume 145 ) 2>&1 )" && fail "упавшее снятие в бакете прошло молча"
  [[ "$err" == *"static-publish.sh --withdraw 145"* ]] || fail "нет команды повтора: $err"
  grep -qE '^145  # ' "$STATIC_EXCLUDE" || fail "строка тома не дописана, хотя бакет упал"
}

# У читальни без своего инструмента выкладки (в платформе его нет) снятие не
# падает: строка в список дописана, а про выложенные сборки сказано словами.
# Читальня без каталога instance/: снятие на сайте уже прошло, и падать
# после него на записи строки нельзя — каталог заводится сам.
test_static_after_takedown_creates_instance_dir() {
  STATIC_EXCLUDE="$TMPROOT/нет-каталога/static-exclude.txt"
  STATIC_PUBLISH="$TMPROOT/нет-такого"
  local rc=0
  ( static_after_takedown volume 145 ) >/dev/null 2>&1 || rc=$?
  (( rc == 0 )) || fail "без каталога списка снятие упало (код $rc)"
  grep -qE '^145  # ' "$STATIC_EXCLUDE" || fail "строка тома не дописана"
}

test_static_after_takedown_without_publisher() {
  STATIC_EXCLUDE="$TMPROOT/exclude.txt"
  STATIC_PUBLISH="$TMPROOT/нет-такого"
  local err rc=0
  err="$( ( static_after_takedown volume 145 ) 2>&1 )" || rc=$?
  (( rc == 0 )) || fail "без инструмента выкладки снятие упало: $err"
  grep -qE '^145  # ' "$STATIC_EXCLUDE" || fail "строка тома не дописана"
  [[ "$err" == *"сами"* ]] || fail "не сказано, что выложенные сборки снимать самим: $err"
}

run_tests
