import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
from urllib.error import HTTPError

from .client import endpoint, read_scan, request_json
from .model import analyze
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
    folder = settings_dir()
    folder.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=folder, delete=False) as stream:
        temp = Path(stream.name)
        try:
            json.dump({"site": site, "token": token}, stream)
            stream.close()
            temp.replace(folder / f"{run_id}.json")
        finally:
            temp.unlink(missing_ok=True)
    print(f"Помощник связан с работой {run_id}.")
    return 0


def run(args):
    config = json.loads((settings_dir() / f"{args.run_id}.json").read_text())
    pages = endpoint(config["site"]) + f"/api/proofread-worker/runs/{args.run_id}/pages"
    token = config["token"]
    model_url = endpoint(args.model_url)
    key = os.getenv("OPENAI_API_KEY")
    after = 0
    failed = False
    while True:
        page = request_json("GET", f"{pages}?after={after}", token=token)
        if page is None:
            return int(failed)
        if page["page_number"] <= after:
            raise ValueError("Сайт вернул неверный номер полосы.")
        after = page["page_number"]
        if page.get("skip_reason") or page["analysis_status"] in ("analyzed", "skipped"):
            continue
        if page["analysis_status"] not in ("failed", "pending"):
            raise ValueError("Сайт вернул неизвестный статус анализа.")
        text = page["base_markdown"]
        scan = None
        if page.get("scan_url") and (args.vision or args.ocr and shutil.which("tesseract")):
            try:
                scan = read_scan(page["scan_url"])
            except (OSError, HTTPException, ValueError):
                print(f"Полоса {after}: скан недоступен.", file=sys.stderr)
        try:
            findings = analyze(model_url, args.model, key, text, scan if args.vision else None)
            if args.ocr and scan:
                findings.extend(ocr_candidates(text, scan[0], args.ocr_language))
            payload = {"findings": findings}
        except (OSError, HTTPException, ValueError, KeyError, IndexError, TypeError) as error:
            # Upstream messages can echo model credentials or page content.
            detail = f"HTTP {error.code}" if isinstance(error, HTTPError) else type(error).__name__
            payload = {"analysis_error": f"Ошибка анализа ({detail})"}
            print(f"Полоса {after}: {payload['analysis_error']}", file=sys.stderr)
            failed = True
        payload["base_sha256"] = page["base_sha256"]
        request_json("PUT", f"{pages}/{page['page_id']}/findings", payload, token)


def main(argv=None):
    parser = argparse.ArgumentParser(prog="proofread-assist")
    commands = parser.add_subparsers(dest="command", required=True)
    bind = commands.add_parser("pair", help="Связать помощник с работой")
    bind.add_argument("code")
    bind.add_argument("--site-url", required=True)
    job = commands.add_parser("run", help="Обработать незавершённые полосы главы")
    job.add_argument("run_id", type=int)
    job.add_argument("--model-url", required=True, help="Базовый URL API, включая /v1 при необходимости")
    job.add_argument("--model", required=True)
    job.add_argument("--ocr", action="store_true", help="Добавить находки Tesseract")
    job.add_argument("--ocr-language", default="rus")
    job.add_argument("--vision", action="store_true", help="Передать скан выбранной модели")
    args = parser.parse_args(argv)
    try:
        return pair(args) if args.command == "pair" else run(args)
    except (OSError, HTTPException, ValueError, KeyError, TypeError) as error:
        detail = f"HTTP {error.code}" if isinstance(error, HTTPError) else type(error).__name__
        print(f"Не удалось выполнить команду ({detail}).", file=sys.stderr)
        return 1
