#!/usr/bin/env bash
# Разворачивание читальни на свежем сервере из снапшота backup.sh.
#
#   ./scripts/deploy.sh root@203.0.113.4 --domain lib.example.org \
#       [--s3-domain scans.example.org] [--acme-email you@example.org] \
#       [--snapshot latest|<имя>|<путь>] [--admin-password <старый>] \
#       [--force-restore] [--skip-dns-check] \
#       [--external-s3]   # хранилище у провайдера, ключи EXTERNAL_S3_* — из .env
#
# Скрипт тонкий намеренно: везёт снапшот и один файл bootstrap.sh, дальше
# работа идёт на сервере под tmux и переживает обрыв связи. Ctrl-C здесь
# закрывает только просмотр лога, прогон продолжается.
#
# Обрыв связи переживает и заливка: rsync повторяется до RSYNC_RETRIES раз и
# докачивает с места обрыва. А вот закрытый терминал или уснувший ноутбук
# убивают сам deploy.sh, поэтому многочасовую заливку запускай в локальном
# tmux:
#
#   tmux new -s deploy './scripts/deploy.sh root@203.0.113.4 --domain lib.example.org …'
#
# Источается тестами — тогда main не запускается.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

REMOTE_ROOT=/opt/proofreader
SESSION=proofreader-bootstrap
# Сколько раз пробовать залить снапшот. Переопределяется окружением —
# этим же пользуются тесты.
RSYNC_RETRIES="${RSYNC_RETRIES:-5}"

TARGET=""; DOMAIN=""; S3_DOMAIN=""; ACME_EMAIL=""; SNAPSHOT_WANT="latest"
ADMIN_OLD_PASSWORD="admin"; FORCE_RESTORE=""; SKIP_DNS=""; EXTERNAL_S3=""

usage() { sed -n '2,13p' "${BASH_SOURCE[0]}" >&2; exit "${1:-1}"; }

deploy_parse_args() {
  TARGET=""; DOMAIN=""; S3_DOMAIN=""; ACME_EMAIL=""; SNAPSHOT_WANT="latest"
  ADMIN_OLD_PASSWORD="admin"; FORCE_RESTORE=""; SKIP_DNS=""; EXTERNAL_S3=""
  while (( $# )); do
    case "$1" in
      --domain)         DOMAIN="${2:-}"; shift 2 ;;
      --s3-domain)      S3_DOMAIN="${2:-}"; shift 2 ;;
      --acme-email)     ACME_EMAIL="${2:-}"; shift 2 ;;
      --snapshot)       SNAPSHOT_WANT="${2:-}"; shift 2 ;;
      --admin-password) ADMIN_OLD_PASSWORD="${2:-}"; shift 2 ;;
      --force-restore)  FORCE_RESTORE=1; shift ;;
      --skip-dns-check) SKIP_DNS=1; shift ;;
      --external-s3)    EXTERNAL_S3=1; shift ;;
      -h | --help)      usage 0 ;;
      -*) die "неизвестный флаг: $1" ;;
      *)  [[ -z "$TARGET" ]] || die "цель уже задана ($TARGET), лишний аргумент: $1"
          TARGET="$1"; shift ;;
    esac
  done
  [[ -n "$TARGET" ]] || die "нужна цель вида root@адрес"
  # bootstrap ставит пакеты и слушает 80/443 — без root там делать нечего,
  # а узнать об этом лучше здесь, чем после часовой заливки 6.5 ГБ.
  [[ "$TARGET" == *@* ]] || die "цель должна быть вида пользователь@адрес (нужен root): получено '$TARGET'"
  [[ -n "$DOMAIN" ]] || die "нужен --domain (например lib.example.org)"
  if [[ -n "$EXTERNAL_S3" ]]; then
    [[ -z "$S3_DOMAIN" ]] ||
      die "--s3-domain при --external-s3 не нужен: браузер ходит прямо во внешнее хранилище"
  else
    : "${S3_DOMAIN:=s3.$DOMAIN}"
  fi
}

# shq <строка> — значение в одинарных кавычках, пригодное для вставки в
# генерируемый run.sh. Апостроф внутри значения (пароль вида it's) иначе
# оборвал бы аргумент, и ошибка вылезла бы уже на сервере, непонятная.
shq() { printf "'%s'" "${1//\'/\'\\\'\'}"; }

# deploy_run_script — текст run.sh для сервера. Отдельной функцией ради
# теста: ключи внешнего хранилища уезжают сюда же, что и старый пароль
# администратора, — в файл 600, а не в командную строку ssh.
deploy_run_script() {
  local storage
  if [[ -n "$EXTERNAL_S3" ]]; then
    storage="--external-s3 $(shq "$EXTERNAL_S3_ENDPOINT") --s3-region $(shq "$EXTERNAL_S3_REGION") --s3-key $(shq "$EXTERNAL_S3_ACCESS_KEY") --s3-secret $(shq "$EXTERNAL_S3_SECRET_KEY")"
  else
    storage="--s3-domain $(shq "$S3_DOMAIN")"
  fi
  cat <<RUN
#!/usr/bin/env bash
set -Eeuo pipefail
bash $REMOTE_ROOT/bootstrap.sh \\
  --domain $(shq "$DOMAIN") $storage --acme-email $(shq "$ACME_EMAIL") \\
  --snapshot $(shq "$REMOTE_ROOT/snapshot") --admin-password $(shq "$ADMIN_OLD_PASSWORD") \\
  ${FORCE_RESTORE:+--force-restore}
RUN
}

# dns_matches <ip> <ответ dig> — построчно, а не подстрокой: 203.0.113.40 содержит
# 203.0.113.4, и наивная проверка пропустила бы чужой сервер.
dns_matches() {
  local ip="$1" answers="$2" line
  while IFS= read -r line; do
    [[ "$line" == "$ip" ]] && return 0
  done <<< "$answers"
  return 1
}

resolve_host() {
  local host="${1#*@}"
  if [[ "$host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '%s\n' "$host"
  else
    dig +short A "$host" | head -1
  fi
}

check_dns() {
  local ip="$1" name answers names=("$DOMAIN")
  # Внешний S3 — поддомена хранилища нет, сверять нечего.
  [[ -z "$S3_DOMAIN" ]] || names+=("$S3_DOMAIN")
  for name in "${names[@]}"; do
    answers="$(dig +short A "$name" || true)"
    dns_matches "$ip" "$answers" ||
      die "A-запись $name не ведёт на $ip (получено: ${answers//$'\n'/, }). Caddy берёт сертификат по HTTP-01 на этот адрес — без записи прогон встанет на выписке. Если перед сервером прокси (Cloudflare), пропусти проверку: --skip-dns-check"
  done
  log "DNS: ${names[*]} ведут на $ip"
}

# cleanup_tail — снять локальный ssh, читающий лог. Переменную coproc bash
# снимает сам, когда процесс пожат, поэтому проверка на пустоту обязательна:
# `kill 0` — это не «никому», а «всей группе процессов», то есть по тому, кто
# запустил deploy.sh.
cleanup_tail() {
  [[ -n "${TAILLOG_PID:-}" ]] || return 0
  kill "$TAILLOG_PID" 2>/dev/null || true
}

# push_snapshot <каталог снапшота> — заливка снапшота с повторами.
#
# Одна попытка на десяток гигабайт через домашний канал — почти гарантированный
# обрыв, а упавший rsync ронял весь deploy.sh. Цена этого была не в самой
# заливке: вход на боевой идёт по паролю, и повторный запуск требовал человека
# у клавиатуры. rsync докачивает — доехавшие файлы пропускаются по размеру и
# mtime, -P держит недокачанный, — поэтому обрыв стоит одной паузы, а не
# заливки заново.
#
# --timeout=120: заглохший канал рвётся сам, чтобы сработал ретрай, а не висел
# до утра (тот же приём, что --speed-limit/--speed-time у curl в
# yadisk-sync.sh). ServerAlive* добивает то, что молчит на уровне ssh, куда
# --timeout самого rsync не дотягивается.
#
# --chown=root:root: rsync иначе сохраняет uid/gid источника, а на стоковой
# Ubuntu это непривилегированный пользователь (uid 1000) — снапшот достался
# бы ему, а не root. --delete: без него повторная заливка более свежего
# снапшота оставляет поверх файлы прошлого; дальше restore.sh делает
# `mc mirror --overwrite --remove`, то есть приводит бакет к состоянию
# СЛИТОГО каталога — объекты, удалённые между снапшотами, тихо вернутся.
push_snapshot() {
  local snap="$1" attempt status
  # Внешний S3: сканы уже у провайдера, files/ на сервер не везём — это
  # десятки гигабайт, которые bootstrap.sh --external-s3 всё равно не тронет.
  local extra=()
  [[ -z "$EXTERNAL_S3" ]] || extra=(--exclude /files/)
  for (( attempt = 1; attempt <= RSYNC_RETRIES; attempt++ )); do
    status=0
    rsync -aP --delete ${extra[@]+"${extra[@]}"} --chown=root:root --timeout=120 \
      -e "ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=4" \
      "$snap/" "$TARGET:$REMOTE_ROOT/snapshot/" || status=$?
    if (( status == 0 )); then
      return 0
    fi
    log "попытка $attempt из $RSYNC_RETRIES оборвалась (rsync $status), докачиваю с места обрыва"
    # Ждать после последней попытки незачем — дальше только die.
    if (( attempt < RSYNC_RETRIES )); then
      sleep $(( attempt * 10 ))
    fi
  done
  die "снапшот не доехал за $RSYNC_RETRIES попыток"
}

main() {
  deploy_parse_args "$@"
  load_config
  if [[ -n "$EXTERNAL_S3" ]]; then
    [[ -n "${EXTERNAL_S3_ENDPOINT:-}" ]] || die "--external-s3: в .env нет EXTERNAL_S3_ENDPOINT (адрес внешнего S3)"
    : "${EXTERNAL_S3_REGION:=default}"
    [[ -n "${EXTERNAL_S3_ACCESS_KEY:-}" && -n "${EXTERNAL_S3_SECRET_KEY:-}" ]] ||
      die "--external-s3: в .env нет EXTERNAL_S3_ACCESS_KEY/EXTERNAL_S3_SECRET_KEY"
  fi

  local snap ip
  snap="$(pick_snapshot "$BACKUP_ROOT" "$SNAPSHOT_WANT")"
  snap="$(cd "$snap" && pwd)"
  [[ -n "$EXTERNAL_S3" || -d "$snap/files" ]] || die "в снапшоте нет files/: $snap"
  [[ -f "$snap/meta.tar" || -f "$snap/db.dump" ]] ||
    die "в снапшоте нет ни meta.tar, ни db.dump: $snap"
  log "снапшот: $snap ($(du -sh "$snap" | cut -f1))"

  ssh -o BatchMode=yes "$TARGET" true ||
    die "ssh до $TARGET не проходит (нужен ключ без пароля)"

  if [[ -z "$SKIP_DNS" ]]; then
    # Без явной проверки отсутствие dig роняет resolve_host под set -e без
    # единого слова — "command not found" из недр подстановки, а не внятный die.
    command -v dig >/dev/null ||
      die "нужен dig для сверки DNS (пакет dnsutils: apt install dnsutils), либо пропусти проверку: --skip-dns-check"
    ip="$(resolve_host "$TARGET")"
    [[ -n "$ip" ]] || die "не удалось определить адрес сервера из '$TARGET'"
    check_dns "$ip"
  else
    log "сверка DNS пропущена (--skip-dns-check)"
  fi

  # rsync и tmux на свежей Ubuntu есть не всегда, а без них ни везти, ни
  # запускать. git ставит уже сам bootstrap, ему он нужен для клона.
  log "готовлю сервер: rsync, tmux"
  ssh "$TARGET" "command -v rsync >/dev/null && command -v tmux >/dev/null ||
    { apt-get update -qq && apt-get install -y -qq rsync tmux; }" ||
    die "не удалось поставить rsync/tmux на сервере"
  # 700, а не дефолтные 755 под root: снапшот несёт bcrypt-хеши паролей всех
  # пользователей (meta.tar), а run.sh ниже — открытым текстом старый пароль
  # администратора. mkdir под root даёт 755, поэтому право выставляется явно.
  ssh "$TARGET" "mkdir -p $REMOTE_ROOT && chmod 700 $REMOTE_ROOT" || die "не создался $REMOTE_ROOT"

  log "везу снапшот (это надолго: $(du -sh "$snap" | cut -f1))"
  push_snapshot "$snap"

  log "везу bootstrap.sh"
  scp -q "$SCRIPT_DIR/bootstrap.sh" "$TARGET:$REMOTE_ROOT/bootstrap.sh" ||
    die "bootstrap.sh не доехал"

  # Лог прошлого прогона обязан уехать в сторону: tail ниже читает файл с начала,
  # и на повторном запуске цикл поймал бы чужой маркер раньше, чем текущий прогон
  # успеет что-то напечатать. Заодно файл создаётся заранее — tail без --retry
  # не стал бы ждать его появления.
  ssh "$TARGET" "mv -f $REMOTE_ROOT/deploy.log $REMOTE_ROOT/deploy.log.prev 2>/dev/null || true;
    : > $REMOTE_ROOT/deploy.log" || die "не удалось подготовить лог на сервере"

  # Команду кладём файлом, а не разворачиваем в кавычках внутри tmux внутри ssh:
  # три уровня цитирования — верный способ однажды передать пустой аргумент.
  log "запускаю разворачивание в tmux-сессии $SESSION"
  # Текст идёт через stdin ssh, а не аргументом: в нём пароль и ключи S3.
  deploy_run_script | ssh "$TARGET" "cat > $REMOTE_ROOT/run.sh" || die "run.sh не доехал"
  # 600, не +x: run.sh несёт старый пароль администратора и ключи внешнего
  # S3 открытым текстом (командная строка bootstrap.sh), а запускается он ниже
  # явным "bash run.sh" — выполнимость файлу не нужна.
  ssh "$TARGET" "chmod 600 $REMOTE_ROOT/run.sh &&
    (tmux has-session -t $SESSION 2>/dev/null && tmux kill-session -t $SESSION) || true;
    tmux new-session -d -s $SESSION \"bash $REMOTE_ROOT/run.sh 2>&1 | tee -a $REMOTE_ROOT/deploy.log\"" ||
    die "не удалось запустить прогон на сервере"

  log "прогон идёт на сервере. Ctrl-C закроет только просмотр лога."
  log "вернуться позже: ssh $TARGET tmux attach -t $SESSION"

  # coproc, а не процесс-подстановка (была здесь раньше по той же причине —
  # while не должен уехать в подоболочку, иначе exit ниже закрыл бы только её):
  # coproc вдобавок даёт PID фонового процесса в $TAILLOG_PID, и есть что убить
  # ловушкой на выход, если поток оборвётся раньше цикла — иначе локальные
  # `ssh ... tail -f` копятся при каждом повторном запуске deploy.sh. exec
  # обязателен: без него $TAILLOG_PID — это pid обёрточной подоболочки coproc,
  # а ssh остаётся её живым потомком и после kill — сама утечка, ради которой
  # всё это писалось, не закрывается. exec заменяет подоболочку самим ssh.
  coproc TAILLOG { exec ssh "$TARGET" "tail -f -n +1 $REMOTE_ROOT/deploy.log"; }
  trap cleanup_tail EXIT

  while IFS= read -r line <&"${TAILLOG[0]}"; do
    printf '%s\n' "$line"
    case "$line" in
      # Отдельный маркер, а не подстрока "ERROR:": она встречается и в выводе
      # вложенных инструментов (например, pg_restore) без провала всего
      # прогона. bootstrap.sh печатает "BOOTSTRAP FAILED" симметрично "DONE"
      # именно на выходе с ненулевым кодом — сама строка ERROR: при этом
      # остаётся в логе строкой(и) выше, оператору видна.
      *"BOOTSTRAP DONE"*) log "готово: https://$DOMAIN"; exit 0 ;;
      *"BOOTSTRAP FAILED"*)
        log "прогон упал (причина — в строке ERROR: выше). Исправь и запусти deploy.sh снова — выполненные фазы пропустятся."
        exit 1 ;;
    esac
  done

  # Поток кончился сам, без единого маркера — типичный обрыв ssh, ради
  # переживания которого всё и живёт в tmux. Молчаливый успешный выход здесь
  # был бы ложным: прогон на сервере, скорее всего, продолжается без нас.
  die "поток лога оборвался раньше BOOTSTRAP DONE/FAILED — похоже, обрыв связи, а не конец прогона. Вернись к нему: ssh $TARGET tmux attach -t $SESSION"
}

# Источается тестами — тогда main не запускается.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
