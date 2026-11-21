import json

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

    assert main(["pair", "once", "--site-url", serve(site)]) == 0
    args = ["run", "7", "--model-url", serve(model) + "/v1", "--model", "volunteer-model"]
    assert main(args) == 1
    assert states == ["analyzed", "pending", "failed", "analyzed"]
    assert main(args) == 0
    assert main(args) == 0
    assert [i for i, body in uploads] == [2, 3, 2]
    assert uploads[-1][1]["findings"] == []
    assert len(model_inputs) == 3
    stored = "".join(f.read_text() for f in tmp_path.iterdir())
    assert "once" not in stored and "MODEL_KEY" not in stored and "scoped" in stored
