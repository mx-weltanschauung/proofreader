from hashlib import sha256
import json


def candidates(text, rows, source="model"):
    if not isinstance(rows, list):
        raise ValueError("Ожидался массив находок.")
    found = {}
    for row in rows:
        if not isinstance(row, dict):
            continue
        start, end = row.get("start_rune"), row.get("end_rune")
        old, new, reason = (row.get(field) for field in ("old", "new", "reason"))
        if not all(isinstance(value, str) for value in (old, new, reason)):
            continue
        if type(start) is not int or type(end) is not int or not 0 <= start <= end <= len(text):
            continue
        if text[start:end] != old or old == new or not reason.strip():
            continue
        identity = json.dumps([start, end, old, new, source]).encode()
        key = sha256(identity).hexdigest()
        item = dict(candidate_key=key, start_rune=start, end_rune=end, old=old,
                    new=new, reason=reason, source=source)
        box = row.get("scan_bbox")
        if isinstance(box, list) and len(box) == 4 and all(type(n) in (float, int) for n in box):
            x, y, w, h = box
            if x >= 0 and y >= 0 and w > 0 and h > 0 and x+w <= 1 and y+h <= 1:
                item["scan_bbox"] = box
        found.setdefault(key, item)
    return list(found.values())
