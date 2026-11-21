import pytest
from urllib.error import HTTPError

from proofread_assist.client import request_json


def test_key_never_sent_to_site(serve):
    calls = []
    site = serve(lambda *args: (calls.append(args) or (204, None)))
    from http.server import BaseHTTPRequestHandler, HTTPServer
    from threading import Thread

    class Redirect(BaseHTTPRequestHandler):
        def do_POST(self):
            self.send_response(302)
            self.send_header("Location", site)
            self.end_headers()

    with HTTPServer(("127.0.0.1", 0), Redirect) as redirect:
        worker = Thread(target=redirect.handle_request)
        worker.start()
        with pytest.raises(HTTPError):
            request_json("POST", f"http://127.0.0.1:{redirect.server_port}", {}, "MODEL_KEY")
        worker.join()
    assert calls == []
