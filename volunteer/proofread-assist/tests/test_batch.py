import json
from pathlib import Path
from subprocess import CompletedProcess

import pytest

from proofread_assist.batch import parse_batch
from proofread_assist.claude import analyze_claude_batch
from proofread_assist.cli import main
from proofread_assist.anthropic import analyze_anthropic_batch
from proofread_assist.codex import analyze_codex_batch


PAGES = [
    {"page_id": 11, "base_markdown": "Ошибка."},
    {"page_id": 12, "base_markdown": "Текст."},
]


def test_batch_requires_exact_page_ids_and_valid_positions():
    payload = {"pages": [
        {"page_id": 12, "findings": []},
        {"page_id": 11, "findings": [{"start_rune": 0, "end_rune": 6,
            "old": "Ошибка", "new": "Описка", "reason": "Скан"}]},
    ]}
    found = parse_batch(PAGES, payload)
    assert found[11][0]["old"] == "Ошибка"
    assert found[12] == []
    for broken in (
        {"pages": payload["pages"][:1]},
        {"pages": [payload["pages"][0]] * 2},
        {"pages": [{"page_id": 13, "findings": []}, payload["pages"][1]]},
        {"pages": [{"page_id": 11, "findings": [{"start_rune": 0,
            "end_rune": 6, "old": "Чужой", "new": "Описка", "reason": "Скан"}]},
            payload["pages"][0]]},
    ):
        with pytest.raises(ValueError):
            parse_batch(PAGES, broken)


def test_claude_cli_uses_subscription_and_one_call_for_two_pages(monkeypatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "API_SECRET")
    monkeypatch.setattr("proofread_assist.claude.shutil.which", lambda _: "/usr/bin/claude")
    calls = []
    pages = [dict(PAGES[0], base_markdown="Осторожно: @/home/user/secret.txt"), PAGES[1]]

    def fake_run(command, **kwargs):
        calls.append(command)
        assert "--safe-mode" in command
        assert command[command.index("--tools") + 1] == ""
        assert kwargs["env"].get("ANTHROPIC_API_KEY") is None
        assert "API_SECRET" not in kwargs["input"]
        assert "@/home/user/secret.txt" not in kwargs["input"]
        assert "\\u0040/home/user/secret.txt" in kwargs["input"]
        assert "11" in kwargs["input"] and "12" in kwargs["input"]
        return CompletedProcess(command, 0, stdout=json.dumps({"structured_output": {
            "pages": [{"page_id": 11, "findings": []},
                      {"page_id": 12, "findings": []}]}}))

    monkeypatch.setattr("proofread_assist.claude.subprocess.run", fake_run)
    assert analyze_claude_batch(pages) == {11: [], 12: []}
    assert len(calls) == 1


@pytest.mark.parametrize("batch_size,expected_calls", [(10, 1), (2, 2)])
def test_run_batches_pages_and_uploads_each_result(
        serve, monkeypatch, tmp_path, batch_size, expected_calls):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    model_calls, uploads = [], []

    def site(method, path, headers, body):
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "site-secret"}
        if method == "GET":
            after = int(path.split("after=")[1])
            if after == 3:
                return 204, None
            return 200, {"page_id": after + 11, "page_number": after + 1,
                         "analysis_status": "pending", "base_markdown": "Текст.",
                         "base_sha256": f"hash-{after}"}
        uploads.append((int(path.split("/")[-2]), body))
        return 204, None

    def model(method, path, headers, body):
        assert path == "/v1/chat/completions"
        assert "site-secret" not in str(body) + str(headers)
        model_calls.append(body)
        content = body["messages"][-1]["content"]
        if isinstance(content, str):
            return 200, {"choices": [{"message": {"content": "[]"}}]}
        prompt = content[0]["text"]
        page_ids = [page["page_id"] for page in json.loads(prompt.split("Полосы:\n", 1)[1])]
        return 200, {"choices": [{"message": {"content": json.dumps({
            "pages": [{"page_id": i, "findings": []} for i in page_ids]})}}]}

    assert main(["pair", "code", "--site-url", serve(site)]) == 0
    assert main(["run", "7", "--model-url", serve(model) + "/v1", "--model", "demo",
                 "--batch-size", str(batch_size)]) == 0
    assert len(model_calls) == expected_calls
    assert [page_id for page_id, _ in uploads] == [11, 12, 13]
    assert [body["base_sha256"] for _, body in uploads] == ["hash-0", "hash-1", "hash-2"]


def test_malformed_batch_is_split_without_cross_page_upload(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    calls, uploads = [], []

    def site(method, path, _headers, body):
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "site-secret"}
        if method == "GET":
            after = int(path.split("after=")[1])
            if after == 2:
                return 204, None
            return 200, {"page_id": after + 11, "page_number": after + 1,
                         "analysis_status": "pending", "base_markdown": "Текст.",
                         "base_sha256": f"hash-{after}"}
        uploads.append(body)
        return 204, None

    def model(_method, _path, _headers, body):
        calls.append(body)
        return 200, {"choices": [{"message": {"content": "{}"}}]}

    assert main(["pair", "code", "--site-url", serve(site)]) == 0
    assert main(["run", "7", "--model-url", serve(model) + "/v1", "--model", "demo"]) == 1
    assert len(calls) == 3
    assert len(uploads) == 2
    assert all("analysis_error" in body for body in uploads)


def test_anthropic_api_uses_native_headers_and_page_schema(monkeypatch):
    def fake_fetch(request, limit):
        assert request.full_url == "https://api.anthropic.com/v1/messages"
        assert request.get_header("X-api-key") == "API_SECRET"
        body = json.loads(request.data)
        assert body["output_config"]["format"]["type"] == "json_schema"
        assert len(body["messages"][0]["content"]) == 1
        return 200, json.dumps({"content": [{"type": "text", "text": json.dumps({
            "pages": [{"page_id": 11, "findings": []},
                      {"page_id": 12, "findings": []}]})}]}).encode(), "application/json"

    monkeypatch.setattr("proofread_assist.anthropic.fetch", fake_fetch)
    assert analyze_anthropic_batch(PAGES, "chosen-model", "API_SECRET") == {11: [], 12: []}


def test_codex_batch_attaches_scans_in_page_order(monkeypatch):
    pages = [dict(PAGES[0], scan=(b"first", "image/png")),
             dict(PAGES[1], scan=(b"second", "image/jpeg"))]
    monkeypatch.setattr("proofread_assist.codex.shutil.which", lambda _: "/usr/bin/codex")

    def fake_run(command, **kwargs):
        paths = [Path(command[i + 1]) for i, arg in enumerate(command) if arg == "-i"]
        assert [path.read_bytes() for path in paths] == [b"first", b"second"]
        assert kwargs["input"].count("page_id") >= 2
        result = Path(command[command.index("--output-last-message") + 1])
        result.write_text(json.dumps({"pages": [
            {"page_id": 11, "findings": []}, {"page_id": 12, "findings": []}]}))
        return CompletedProcess(command, 0)

    monkeypatch.setattr("proofread_assist.codex.subprocess.run", fake_run)
    assert analyze_codex_batch(pages) == {11: [], 12: []}


def test_batch_size_is_volunteer_controlled(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    requests = []

    def site(_method, path, _headers, _body):
        requests.append(path)
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "secret"}
        return 204, None

    assert main(["pair", "code", "--site-url", serve(site)]) == 0
    assert main(["run", "7", "--claude-api", "--model", "chosen-model",
                 "--batch-size", "21"]) == 1
    assert len(requests) == 1


def test_rate_limit_does_not_multiply_model_requests(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    calls, uploads = [], []

    def site(method, path, _headers, body):
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "secret"}
        if method == "GET":
            after = int(path.split("after=")[1])
            if after == 2:
                return 204, None
            return 200, {"page_id": after + 11, "page_number": after + 1,
                         "analysis_status": "pending", "base_markdown": "Текст.",
                         "base_sha256": "hash"}
        uploads.append(body)
        return 204, None

    def model(_method, _path, _headers, body):
        calls.append(body)
        return 429, {"error": "busy"}

    assert main(["pair", "code", "--site-url", serve(site)]) == 0
    assert main(["run", "7", "--model-url", serve(model) + "/v1", "--model", "demo"]) == 1
    assert len(calls) == 1
    assert len(uploads) == 2 and all("analysis_error" in body for body in uploads)
