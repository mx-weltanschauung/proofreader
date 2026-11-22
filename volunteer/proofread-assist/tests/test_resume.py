import json
import os
import subprocess

from proofread_assist.cli import main


def test_retries_one_failed_page(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    monkeypatch.setenv("OPENAI_API_KEY", "MODEL_KEY")
    states = ["analyzed", "pending", "failed", "pending"]
    uploads, model_inputs = [], []
    fail = True

    def site(method, path, headers, body):
        assert "MODEL_KEY" not in str(headers) + str(body)
        if path.endswith("/pair"):
            assert body == {"code": "once"}
            return 200, {"run_id": 7, "token": "scoped"}
        assert headers["Authorization"] == "Bearer scoped"
        if method == "GET":
            i = int(path.split("after=")[1])
            if i == len(states):
                return 204, None
            return 200, dict(page_id=i+10, page_number=i+1, analysis_status=states[i],
                             base_markdown="полоса", base_sha256="hash",
                             skip_reason="closed" if i == 1 else "")
        i = int(path.split("/")[-2]) - 10
        assert body["base_sha256"] == "hash"
        states[i] = "failed" if "analysis_error" in body else "analyzed"
        uploads.append((i, body))
        return 204, None

    def model(method, path, headers, body):
        nonlocal fail
        assert path == "/v1/chat/completions"
        assert headers["Authorization"] == "Bearer MODEL_KEY"
        assert body["model"] == "volunteer-model"
        assert isinstance(body["messages"][-1]["content"], str)
        model_inputs.append(body)
        if fail:
            fail = False
            return 503, {"error": "MODEL_KEY"}
        return 200, {"choices": [{"message": {"content": json.dumps([])}}]}

    def invoke(argv):
        binary = os.getenv("PROOFREAD_ASSIST_BINARY")
        if binary:
            return subprocess.run([binary, *argv], timeout=60).returncode
        return main(argv)

    assert invoke(["pair", "once", "--site-url", serve(site)]) == 0
    args = ["run", "7", "--model-url", serve(model) + "/v1", "--model", "volunteer-model"]
    assert invoke(args) == 1
    assert states == ["analyzed", "pending", "failed", "analyzed"]
    assert invoke(args) == 0
    assert invoke(args) == 0
    assert [i for i, body in uploads] == [2, 3, 2]
    assert uploads[-1][1]["findings"] == []
    assert len(model_inputs) == 3
    stored = "".join(f.read_text() for f in tmp_path.iterdir())
    assert "once" not in stored and "MODEL_KEY" not in stored and "scoped" in stored


def test_keyed_model_cannot_use_site_origin(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    monkeypatch.setenv("OPENAI_API_KEY", "MODEL_KEY")
    requests = []

    def respond(method, path, headers, body):
        requests.append((path, headers.get("Authorization")))
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "scoped"}
        if path.endswith("after=0"):
            return 200, dict(page_id=10, page_number=1, analysis_status="pending",
                             base_markdown="полоса", base_sha256="hash")
        if path.endswith("/chat/completions"):
            return 200, {"choices": [{"message": {"content": "[]"}}]}
        return 204, None

    url = serve(respond)
    assert main(["pair", "once", "--site-url", url + "/library"]) == 0
    status = main(["run", "7", "--model-url", url + "/model/v1", "--model", "local"])
    assert requests == [("/library/api/proofread-worker/pair", None)]
    assert status == 1


def test_configure_probes_before_saving_and_keeps_key_local(serve, monkeypatch, tmp_path):
    monkeypatch.setenv("PROOFREAD_ASSIST_HOME", str(tmp_path))
    monkeypatch.setenv("OPENAI_API_KEY", "MODEL_KEY")
    site_requests, model_inputs = [], []
    ready = False

    def site(method, path, headers, body):
        assert "MODEL_KEY" not in str((path, headers, body))
        site_requests.append(path)
        if path.endswith("/pair"):
            return 200, {"run_id": 7, "token": "worker"}
        return 204, None

    def model(method, path, headers, body):
        assert headers["Authorization"] == "Bearer MODEL_KEY"
        model_inputs.append(body["messages"][-1]["content"])
        if not ready:
            return 503, {}
        return 200, {"choices": [{"message": {"content": "[]"}}]}

    site_url, model_url = serve(site), serve(model)
    assert main(["pair", "once", "--site-url", site_url]) == 0
    args = ["configure", "7", "--model", "reader-model", "--model-url"]
    assert main(args + [site_url]) == 1
    assert main(args + [model_url]) == 1
    assert main(["run", "7"]) == 1
    assert site_requests == ["/api/proofread-worker/pair"]
    assert not (tmp_path / "model.json").exists()
    ready = True
    assert main(args + [model_url]) == 0
    assert len(model_inputs) == 2
    assert json.loads((tmp_path / "model.json").read_text()) == {
        "model_url": model_url, "model": "reader-model"}
    assert main(["run", "7"]) == 0
    assert site_requests[-1].endswith("7/pages?after=0")
