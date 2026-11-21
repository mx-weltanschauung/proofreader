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


def test_origin_uses_default_port_and_normalizes_hostname():
    from proofread_assist.client import origin

    assert origin("HTTP://EXAMPLE.org/site") == origin("http://example.org:80/v1")
    assert origin("https://example.org") == origin("https://EXAMPLE.org:443/v1")
    assert origin("https://example.org") != origin("https://example.org:444")
    assert origin("http://example.org:443") != origin("https://example.org:443")
