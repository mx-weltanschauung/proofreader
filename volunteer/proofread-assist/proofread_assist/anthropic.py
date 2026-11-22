"""Native Anthropic Messages API executor."""

import base64
import json
from urllib.request import Request

from .batch import BATCH_SCHEMA, page_prompt, parse_batch
from .client import fetch


def analyze_anthropic_batch(pages, model, key):
    if not key:
        raise ValueError("Задайте ANTHROPIC_API_KEY для --claude-api.")
    content = [{"type": "text", "text": page_prompt(pages)}]
    for page in pages:
        if page.get("scan"):
            data, mime = page["scan"]
            content.append({"type": "text", "text": f"Скан полосы page_id={page['page_id']}:"})
            content.append({"type": "image", "source": {
                "type": "base64", "media_type": mime,
                "data": base64.b64encode(data).decode("ascii")}})
    body = {"model": model, "max_tokens": 8192,
            "messages": [{"role": "user", "content": content}],
            "output_config": {"format": {"type": "json_schema", "schema": BATCH_SCHEMA}}}
    request = Request("https://api.anthropic.com/v1/messages",
                      json.dumps(body, ensure_ascii=False).encode("utf-8"),
                      {"Content-Type": "application/json", "Accept": "application/json",
                       "x-api-key": key, "anthropic-version": "2023-06-01"}, method="POST")
    _, data, _ = fetch(request, 16 << 20)
    response = json.loads(data)
    if response.get("stop_reason") == "max_tokens":
        raise ValueError("Ответ Claude не поместился в лимит; пакет будет разделён.")
    blocks = response.get("content", [])
    if not blocks or blocks[0].get("type") != "text":
        raise ValueError("Claude API не вернул пакет полос.")
    return parse_batch(pages, json.loads(blocks[0]["text"]))
