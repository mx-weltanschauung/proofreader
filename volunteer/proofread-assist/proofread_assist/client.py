import json
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def endpoint(url):
    parts = urlsplit(url)
    if (parts.scheme not in {"http", "https"} or not parts.hostname
            or parts.username is not None or parts.fragment):
        raise ValueError("Ожидался адрес HTTP(S) без встроенного логина.")
    return url.rstrip("/")


def fetch(request, limit):
    endpoint(request.full_url)
    with build_opener(NoRedirect()).open(request, timeout=120) as response:
        data = response.read(limit + 1)
        if len(data) > limit:
            raise ValueError("Ответ превышает допустимый размер.")
        return response.status, data, response.headers.get_content_type()


def request_json(method, url, body=None, token=None):
    headers = {"Accept": "application/json"}
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode()
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    status, result, _ = fetch(Request(url, data, headers, method=method), 16 << 20)
    return None if status == 204 else json.loads(result)


def read_scan(url):
    _, data, mime = fetch(Request(url), 32 << 20)
    return data, mime
