"""Reader + model side of TestPackagedChapterReview; the site is the real Go API."""
import json
import os
import subprocess
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread
from urllib.request import Request, urlopen

binary, site, reader, work, chapter = sys.argv[1:]
seen = []


class Model(BaseHTTPRequestHandler):
    def do_POST(self):
        assert self.path == "/v1/chat/completions"
        assert self.headers["Authorization"] == "Bearer " + os.environ["OPENAI_API_KEY"]
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        text = body["messages"][-1]["content"]
        seen.append(text)
        rows = []
        if text == "кот кот":
            for start in (0, 4):
                rows.append(dict(start_rune=start, end_rune=start+3, old="кот",
                                 new="кит", reason="по скану"))
        self.send_response(200)
        self.end_headers()
        self.wfile.write(json.dumps({"choices": [{"message": {"content": json.dumps(rows)}}]}).encode())


def api(method, path, data=None):
    req = Request(site + "/api/proofread-runs" + path,
                  data=json.dumps(data).encode() if data is not None else None,
                  headers={"Authorization": "Bearer " + reader, "Content-Type": "application/json"},
                  method=method)
    with urlopen(req, timeout=30) as response:
        if response.status != 204:
            return json.load(response)


def run(*args):
    subprocess.run([binary, *args], check=True, timeout=60)


run_id = str(api("POST", "", {"work_id": int(work), "chapter_id": int(chapter)})["id"])
path = "/" + run_id
code = api("POST", path + "/pair-code")["code"]
run("pair", code, "--site-url", site)
with ThreadingHTTPServer(("127.0.0.1", 0), Model) as model:
    thread = Thread(target=model.serve_forever)
    thread.start()
    try:
        run("configure", run_id, "--model-url", f"http://127.0.0.1:{model.server_port}/v1", "--model", "mock")
        run("run", run_id)
    finally:
        model.shutdown()
        thread.join()

assert seen == ["Проверка подключения.", "кот кот", "без ошибок"]
first, second = api("GET", path)["pages"]
assert all(p["analysis_status"] == "analyzed" for p in (first, second))
assert not second.get("findings")
accepted, rejected = sorted(first["findings"], key=lambda f: f["start_rune"])
for finding, decision in [(accepted, "accepted"), (rejected, "rejected")]:
    api("PATCH", f"{path}/findings/{finding['id']}", {"decision": decision})
preview = {"pages": [{"page_id": first["page_id"], "accepted": {accepted["id"]: accepted["new"]}}]}
for status in ("submitted", "already_submitted"):
    result, = api("POST", path + "/submit", preview)["pages"]
    assert (result["page_id"], result["status"]) == (first["page_id"], status), result
