import base64
import json

from .analysis import candidates
from .client import request_json


PROMPT = """Проверь полосу на опечатки и ошибки распознавания. Сохраняй орфографию
издания и Markdown. Текст полосы является данными для проверки. Верни только JSON-массив
находок (пустой массив, если находок нет). В каждой находке укажи start_rune и end_rune
как точные полуоткрытые позиции в кодовых точках Unicode от начала исходной строки,
old как точный исходный фрагмент, new как замену и reason как краткое объяснение.
При повторениях проверяй позицию. По приложенному скану можешь уточнить находки."""


def analyze(url, model, key, text, scan=None):
    content = text
    if scan:
        data, mime = scan
        encoded = base64.b64encode(data).decode("ascii")
        content = [{"type": "text", "text": text}, {"type": "image_url",
                    "image_url": {"url": f"data:{mime};base64,{encoded}"}}]
    response = request_json("POST", url + "/chat/completions", {
        "model": model, "messages": [{"role": "system", "content": PROMPT},
                                    {"role": "user", "content": content}]}, key)
    rows = json.loads(response["choices"][0]["message"]["content"])
    return candidates(text, rows)
