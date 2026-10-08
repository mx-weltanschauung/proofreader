#!/usr/bin/env bash
# Тесты повторов заливки снапшота (push_snapshot из scripts/deploy.sh).
# Запуск: ./scripts/tests/deploy_push_snapshot_test.sh — ни ssh, ни сети,
# ни настоящего rsync не нужно: rsync подставной, лежит в $PATH.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../deploy.sh
source "$TESTS_DIR/../deploy.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

# fake_rsync <сколько первых попыток провалить> — кладёт в $TMPROOT/bin свой
# rsync и двигает $PATH. Каждый вызов дописывает строку в $TMPROOT/calls, по
# ней и считается число попыток. Аргументы вызова пишутся туда же — тест
# проверяет по ним, что докачка и обрыв заглохшего канала не потерялись.
fake_rsync() {
  local fail_first="$1"
  mkdir -p "$TMPROOT/bin"
  cat > "$TMPROOT/bin/rsync" <<SCRIPT
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$TMPROOT/calls"
n=\$(wc -l < "$TMPROOT/calls")
(( n <= $fail_first )) && exit 12
exit 0
SCRIPT
  chmod +x "$TMPROOT/bin/rsync"
  PATH="$TMPROOT/bin:$PATH"
}

calls() { [[ -f "$TMPROOT/calls" ]] && wc -l < "$TMPROOT/calls" | tr -d ' ' || printf '0'; }

setup() {
  TARGET="root@203.0.113.4"
  # Паузы между попытками не нужны тесту: он проверяет число попыток, а не
  # терпение. Без подмены набор ждал бы 10+20+30 секунд на ровном месте.
  sleep() { :; }
}

test_a_transfer_that_succeeds_at_once_is_not_repeated() {
  setup; fake_rsync 0
  RSYNC_RETRIES=5
  push_snapshot "$TMPROOT" >/dev/null || fail "успешная заливка не должна падать"
  [[ "$(calls)" == 1 ]] || fail "успех с первого раза не должен повторяться, вызовов: $(calls)"
}

test_a_broken_transfer_is_retried_until_it_succeeds() {
  setup; fake_rsync 2
  RSYNC_RETRIES=5
  push_snapshot "$TMPROOT" >/dev/null || fail "заливка обязана дожить до успешной попытки"
  [[ "$(calls)" == 3 ]] || fail "два обрыва подряд — три вызова rsync, получено: $(calls)"
}

# Ровно та регрессия, ради которой всё писалось: раньше единственный обрыв
# ронял deploy.sh, и повторный запуск упирался в пароль от боевого.
#
# Вызов идёт отдельным процессом, а не `push_snapshot ... || rc=$?` в этом же
# шелле, и это не перестраховка. Функция, запущенная слева от ||, выполняется
# с погашенным set -e во всём теле: любой преждевременный выход по set -e
# внутри неё стал бы тесту не виден, и набор прошёл бы на сломанном коде.
# main() зовёт push_snapshot голой командой, где set -e жив; ровно это и
# воспроизводит run.sh.
run_push_snapshot() {
  cat > "$TMPROOT/run.sh" <<SCRIPT
source "$TESTS_DIR/../deploy.sh"
TARGET="root@203.0.113.4"
RSYNC_RETRIES=$1
sleep() { :; }
push_snapshot "$TMPROOT"
SCRIPT
  bash "$TMPROOT/run.sh" 2>&1
}

test_the_last_attempt_still_reaches_die_instead_of_exiting_early() {
  setup; fake_rsync 99
  local out rc=0
  out="$(run_push_snapshot 3)" || rc=$?
  (( rc != 0 )) || fail "исчерпав попытки, заливка обязана провалиться"
  [[ "$out" == *"за 3 попыток"* ]] ||
    fail "выход обязан идти через die с числом попыток, а не молча по set -e; получено: $out"
  [[ "$(calls)" == 3 ]] || fail "должно быть ровно RSYNC_RETRIES попыток, получено: $(calls)"
}

test_the_transfer_resumes_and_kills_a_stalled_channel() {
  setup; fake_rsync 0
  RSYNC_RETRIES=5
  push_snapshot "$TMPROOT" >/dev/null || fail "заливка не должна падать"
  local args; args="$(cat "$TMPROOT/calls")"
  [[ "$args" == *"-aP"* ]] ||
    fail "без -P недокачанный файл выбрасывается и повтор везёт его заново: $args"
  [[ "$args" == *"--timeout=120"* ]] ||
    fail "без --timeout заглохший канал висит до утра и ретрай не срабатывает: $args"
  [[ "$args" == *"--chown=root:root"* ]] ||
    fail "без --chown снапшот достанется uid 1000, а не root: $args"
  [[ "$args" == *"--delete"* ]] ||
    fail "без --delete файлы прошлого снапшота вернутся в бакет через mc mirror: $args"
}

# Внешний S3: сканы уже у провайдера, гигабайты files/ на сервер не едут.
test_external_s3_does_not_push_scans() {
  setup; fake_rsync 0
  EXTERNAL_S3=1
  push_snapshot "$TMPROOT" >/dev/null || fail "заливка не должна падать"
  grep -q -- '--exclude /files/' "$TMPROOT/calls" || fail "files/ обязан исключаться: $(cat "$TMPROOT/calls")"
  EXTERNAL_S3=""
}

test_local_storage_pushes_scans() {
  setup; fake_rsync 0
  EXTERNAL_S3=""
  push_snapshot "$TMPROOT" >/dev/null || fail "заливка не должна падать"
  if grep -q -- '--exclude' "$TMPROOT/calls"; then
    fail "местный режим везёт снапшот целиком: $(cat "$TMPROOT/calls")"
  fi
  return 0
}

run_tests
