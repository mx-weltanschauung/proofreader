import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
from urllib.error import HTTPError

from . import __version__
from .client import endpoint, origin, read_scan, request_json
from .codex import CodexError, analyze_codex
from .codex import analyze_codex_batch
from .claude import ClaudeError, analyze_claude_batch
from .anthropic import analyze_anthropic_batch
from .model import analyze, analyze_batch
from .ocr import ocr_candidates


def settings_dir():
    override = os.getenv("PROOFREAD_ASSIST_HOME")
    if override:
        return Path(override)
    base = os.getenv("APPDATA") if os.name == "nt" else os.getenv("XDG_CONFIG_HOME")
    return (Path(base) if base else Path.home() / ".config") / "proofread-assist"


def pair(args):
    site = endpoint(args.site_url)
    reply = request_json("POST", site + "/api/proofread-worker/pair", {"code": args.code})
    run_id, token = reply["run_id"], reply["token"]
    if type(run_id) is not int or not isinstance(token, str) or not token:
        raise ValueError("Некорректный ответ привязки.")
    save_settings(f"{run_id}.json", {"site": site, "token": token})
    print(f"Помощник связан с работой {run_id}.")
    return 0


def save_settings(name, values):
    folder = settings_dir()
    folder.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=folder, delete=False) as stream:
        temp = Path(stream.name)
        try:
            json.dump(values, stream)
            stream.close()
            temp.replace(folder / name)
        finally:
            temp.unlink(missing_ok=True)


class ModelConfigurationError(ValueError):
    pass


def model_connection(site, url):
    model_url = endpoint(url)
    if "?" in model_url:
        raise ModelConfigurationError("Укажите базовый адрес модели без параметров запроса.")
    key = os.getenv("OPENAI_API_KEY")
    if key and origin(model_url) == origin(site):
        raise ModelConfigurationError("Для модели с ключом укажите отдельный от сайта адрес.")
    return model_url, key


def configure(args):
    config = json.loads((settings_dir() / f"{args.run_id}.json").read_text())
    model_url, key = model_connection(config["site"], args.model_url)
    analyze(model_url, args.model, key, "Проверка подключения.")
    save_settings("model.json", {"model_url": model_url, "model": args.model})
    print("Подключение проверено. Настройки модели сохранены.")
    return 0


def run(args):
    config = json.loads((settings_dir() / f"{args.run_id}.json").read_text())
    pages = endpoint(config["site"]) + f"/api/proofread-worker/runs/{args.run_id}/pages"
    token = config["token"]
    if not 1 <= args.batch_size <= 20:
        print("Размер пакета должен быть от 1 до 20 полос.", file=sys.stderr)
        return 1
    if args.codex or args.claude:
        if args.model_url is not None or (args.codex and args.model is not None):
            print("Для CLI не указывайте --model-url; --model допустим только с --claude.",
                  file=sys.stderr)
            return 1
        executable = "codex" if args.codex else "claude"
        if not shutil.which(executable):
            print(f"{executable} CLI не найден в PATH.", file=sys.stderr)
            return 1
        model_url = key = model = None
    elif args.claude_api:
        if args.model_url is not None or not args.model:
            print("С --claude-api укажите --model без --model-url.", file=sys.stderr)
            return 1
        key = os.getenv("ANTHROPIC_API_KEY")
        if not key:
            print("Задайте ANTHROPIC_API_KEY для --claude-api.", file=sys.stderr)
            return 1
        model_url, model = None, args.model
    elif args.model_url is None and args.model is None:
        try:
            model_settings = json.loads((settings_dir() / "model.json").read_text())
        except FileNotFoundError:
            print("Сначала выполните configure или укажите --model-url и --model.", file=sys.stderr)
            return 1
        url, model = model_settings["model_url"], model_settings["model"]
        model_url, key = model_connection(config["site"], url)
    elif args.model_url and args.model:
        url, model = args.model_url, args.model
        model_url, key = model_connection(config["site"], url)
    else:
        print("Укажите --model-url и --model вместе.", file=sys.stderr)
        return 1
    after = 0
    failed = False
    pending = []

    def analyze_group(group):
        if len(group) == 1 and args.codex:
            page = group[0]
            return {page["page_id"]: analyze_codex(page["base_markdown"], page.get("scan"))}
        if len(group) == 1 and not (args.claude or args.claude_api):
            page = group[0]
            return {page["page_id"]: analyze(model_url, model, key, page["base_markdown"],
                                              page.get("scan"))}
        if args.codex:
            return analyze_codex_batch(group)
        if args.claude:
            return analyze_claude_batch(group, args.model)
        if args.claude_api:
            return analyze_anthropic_batch(group, model, key)
        return analyze_batch(model_url, model, key, group)

    def upload(page, findings=None, error=None):
        nonlocal failed
        number = page["page_number"]
        if error is None:
            try:
                if args.ocr and page.get("ocr_scan"):
                    findings.extend(ocr_candidates(page["base_markdown"], page["ocr_scan"][0],
                                                   args.ocr_language))
            except (OSError, ValueError) as exc:
                error = exc
        if error is None:
            payload = {"findings": findings, "base_sha256": page["base_sha256"]}
            message = f"Полоса {number}: находок {len(findings)}."
        else:
            detail = (str(error) if isinstance(error, (CodexError, ClaudeError)) else
                      f"HTTP {error.code}" if isinstance(error, HTTPError) else
                      type(error).__name__)
            payload = {"analysis_error": f"Ошибка анализа ({detail})",
                       "base_sha256": page["base_sha256"]}
            message = f"Полоса {number}: {payload['analysis_error']}"
            failed = True
        request_json("PUT", f"{pages}/{page['page_id']}/findings", payload, token)
        print(message, file=sys.stderr if error is not None else sys.stdout, flush=True)

    def process(group):
        if len(group) > 1:
            print(f"Анализ полос {group[0]['page_number']}–{group[-1]['page_number']}…", flush=True)
        try:
            found = analyze_group(group)
        except (OSError, HTTPException, ValueError, KeyError, IndexError, TypeError) as error:
            # Authentication and rate-limit errors will not improve when retried
            # as smaller groups; avoid multiplying requests to a busy service.
            retry_smaller = not isinstance(error, HTTPError) or error.code in (400, 413, 422)
            if len(group) > 1 and retry_smaller:
                middle = len(group) // 2
                process(group[:middle])
                process(group[middle:])
            else:
                for page in group:
                    upload(page, error=error)
            return
        for page in group:
            upload(page, found[page["page_id"]])

    text_size = scan_size = 0
    while True:
        page = request_json("GET", f"{pages}?after={after}", token=token)
        if page is None:
            if pending:
                process(pending)
            return int(failed)
        if page["page_number"] <= after:
            raise ValueError("Сайт вернул неверный номер полосы.")
        after = page["page_number"]
        if page.get("skip_reason") or page["analysis_status"] in ("analyzed", "skipped"):
            continue
        if page["analysis_status"] not in ("failed", "pending"):
            raise ValueError("Сайт вернул неизвестный статус анализа.")
        scan = None
        if page.get("scan_url") and (args.vision or args.ocr and shutil.which("tesseract")):
            try:
                scan = read_scan(page["scan_url"])
            except (OSError, HTTPException, ValueError):
                print(f"Полоса {after}: скан недоступен.", file=sys.stderr)
        page["scan"] = scan if args.vision else None
        page["ocr_scan"] = scan if args.ocr else None
        page_text_size = len(page["base_markdown"])
        page_scan_size = len(scan[0]) if scan else 0
        if pending and (len(pending) >= args.batch_size or text_size + page_text_size > 60000
                        or scan_size + page_scan_size > 24 << 20):
            process(pending)
            pending = []
            text_size = scan_size = 0
        pending.append(page)
        text_size += page_text_size
        scan_size += page_scan_size
        if len(pending) >= args.batch_size:
            process(pending)
            pending = []
            text_size = scan_size = 0


def main(argv=None):
    parser = argparse.ArgumentParser(prog="proofread-assist")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    commands = parser.add_subparsers(dest="command", required=True)
    bind = commands.add_parser("pair", help="Связать помощник с работой")
    bind.add_argument("code")
    bind.add_argument("--site-url", required=True)
    setup = commands.add_parser("configure", help="Проверить модель и сохранить настройки")
    setup.add_argument("run_id", type=int)
    setup.add_argument("--model-url", required=True)
    setup.add_argument("--model", required=True)
    job = commands.add_parser("run", help="Обработать незавершённые полосы главы")
    job.add_argument("run_id", type=int)
    job.add_argument("--model-url", help="Базовый URL API, включая /v1 при необходимости")
    job.add_argument("--model")
    job.add_argument("--codex", action="store_true", help="Вызвать Codex CLI с текущим входом ChatGPT")
    job.add_argument("--claude", action="store_true", help="Вызвать Claude Code CLI с текущим входом")
    job.add_argument("--claude-api", action="store_true", help="Вызвать Anthropic API с ключом")
    job.add_argument("--batch-size", type=int, default=10, help="Полос за вызов модели: 1–20 (по умолчанию 10)")
    job.add_argument("--ocr", action="store_true", help="Добавить находки Tesseract")
    job.add_argument("--ocr-language", default="rus")
    job.add_argument("--vision", action="store_true", help="Передать скан выбранной модели")
    args = parser.parse_args(argv)
    if args.command == "run" and sum((args.codex, args.claude, args.claude_api)) > 1:
        parser.error("Выберите только один из --codex, --claude и --claude-api.")
    try:
        return {"pair": pair, "configure": configure, "run": run}[args.command](args)
    except ModelConfigurationError as error:
        print(error, file=sys.stderr)
        return 1
    except (OSError, HTTPException, ValueError, KeyError, IndexError, TypeError) as error:
        detail = f"HTTP {error.code}" if isinstance(error, HTTPError) else type(error).__name__
        print(f"Не удалось выполнить команду ({detail}).", file=sys.stderr)
        return 1
