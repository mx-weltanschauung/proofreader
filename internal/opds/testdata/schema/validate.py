#!/usr/bin/env python3
"""Валидирует ленту OPDS 1.2 против RELAX NG схемы каталога.

opds.rng и atom.rng получены из opds.rnc (github.com/opds-community/specs,
schema/1.2) и RELAX NG Compact схемы из приложения B RFC 4287 конвертером
trang (org.relaxng:trang:20241231). В opds.rng одна правка после
конвертации, см. README.md рядом.

Использование: validate.py [путь]; без аргумента читает stdin.
Код возврата: 0 — валидна, 1 — нет или не разбирается. Ошибки — в stderr.
"""
import sys
from pathlib import Path

from lxml import etree


def main() -> int:
    here = Path(__file__).resolve().parent
    schema = etree.RelaxNG(etree.parse(str(here / "opds.rng")))
    data = Path(sys.argv[1]).read_bytes() if len(sys.argv) > 1 else sys.stdin.buffer.read()
    try:
        doc = etree.fromstring(data)
    except etree.XMLSyntaxError as e:
        print(f"XML не разбирается: {e}", file=sys.stderr)
        return 1
    if schema.validate(doc):
        return 0
    for err in schema.error_log:
        print(f"{err.line}:{err.column}: {err.message}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
