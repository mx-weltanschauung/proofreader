"""Signed-in Claude Code CLI executor."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from .batch import BATCH_SCHEMA, page_prompt, parse_batch


class ClaudeError(ValueError):
    """Safe-to-display CLI error."""


def analyze_claude_batch(pages, model=None):
    executable = shutil.which("claude")
    if not executable:
        raise ClaudeError("Claude Code CLI не найден в PATH.")
    with tempfile.TemporaryDirectory(prefix="proofread-claude-") as directory:
        # Claude Code expands @paths even with tools disabled. Keep @ in page text
        # as a JSON escape, leaving only our own temporary scan references literal.
        prompt = page_prompt(pages).replace("@", "\\u0040")
        for index, page in enumerate(pages):
            if page.get("scan"):
                data, mime = page["scan"]
                extension = {"image/png": ".png", "image/jpeg": ".jpg",
                             "image/webp": ".webp"}.get(mime)
                if not extension:
                    raise ClaudeError("Формат скана не поддерживается Claude CLI.")
                image = Path(directory) / f"scan-{index}{extension}"
                image.write_bytes(data)
                prompt += f"\nСкан полосы page_id={page['page_id']}: @{image}"
        command = [executable, "-p", "--safe-mode", "--tools", "",
                   "--no-session-persistence", "--output-format", "json",
                   "--json-schema", json.dumps(BATCH_SCHEMA)]
        if model:
            command += ["--model", model]
        env = os.environ.copy()
        env.pop("ANTHROPIC_API_KEY", None)
        env.pop("OPENAI_API_KEY", None)
        try:
            completed = subprocess.run(command, input=prompt, text=True, encoding="utf-8",
                                       capture_output=True, timeout=900, env=env, cwd=directory)
            if completed.returncode != 0:
                raise ClaudeError("Claude Code CLI завершился с ошибкой.")
            response = json.loads(completed.stdout)
            if response.get("is_error") or not isinstance(response.get("structured_output"), dict):
                raise ClaudeError("Claude Code CLI не вернул корректный пакет полос.")
            return parse_batch(pages, response["structured_output"])
        except (OSError, subprocess.TimeoutExpired, json.JSONDecodeError) as error:
            raise ClaudeError("Claude Code CLI не вернул корректный пакет полос.") from error
