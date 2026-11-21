import csv
from difflib import SequenceMatcher
from io import StringIO
import re
import shutil
import subprocess

from .analysis import candidates


def ocr_candidates(text, scan, language="rus"):
    executable = shutil.which("tesseract")
    if not executable:
        return []
    try:
        result = subprocess.run([executable, "stdin", "stdout", "-l", language, "tsv"],
                                input=scan, capture_output=True, check=True, timeout=60)
        return from_tsv(text, result.stdout.decode("utf-8"))
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, csv.Error):
        return []


def from_tsv(text, output):
    words = []
    width = height = 0
    for row in csv.DictReader(StringIO(output), delimiter="\t", quoting=csv.QUOTE_NONE):
        if row["level"] == "1":
            width, height = int(row["width"]), int(row["height"])
        if row["level"] == "5" and row["text"]:
            words.append(row)
    if width <= 0 or height <= 0:
        return []
    spans = list(re.finditer(r"\S+", text))
    base = [span.group() for span in spans]
    scanned = [word["text"] for word in words]
    edits = []
    for tag, i, j, k, l in SequenceMatcher(None, base, scanned, autojunk=False).get_opcodes():
        if tag != "replace" or j-i != 1 or l-k != 1:
            continue
        # Require a unique pair of matching neighbours before locating an OCR word.
        if i == 0 or j == len(base) or k == 0 or l == len(scanned):
            continue
        neighbours = base[i-1], base[j]
        if neighbours != (scanned[k-1], scanned[l]):
            continue
        if sum(pair == neighbours for pair in zip(base, base[2:])) != 1:
            continue
        if sum(pair == neighbours for pair in zip(scanned, scanned[2:])) != 1:
            continue
        row = words[k]
        if float(row["conf"]) < 75:
            continue
        box = [int(row["left"])/width, int(row["top"])/height,
               int(row["width"])/width, int(row["height"])/height]
        edits.append(dict(start_rune=spans[i].start(), end_rune=spans[i].end(),
                          old=base[i], new=scanned[k], reason="Расхождение со сканом по OCR",
                          scan_bbox=box))
    return candidates(text, edits, "ocr")
