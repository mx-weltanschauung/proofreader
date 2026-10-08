#!/usr/bin/env python3
"""Валидирует FB2-документ против настоящей схемы FictionBook.xsd.

Схема вендорена рядом (FictionBook.xsd + её импорты FictionBookGenres.xsd,
FictionBookLang.xsd, FictionBookLinks.xsd — взяты из
https://github.com/gribuser/fb2, лицензия в LICENSE.md). FictionNotes.xsd из
того же репозитория сюда не входит: у неё другой корневой элемент
(FictionBookMarkup, заметки/выделения читалки), она не участвует в проверке
самого FB2-документа.

Это сильнее, чем xml.Unmarshal в тестах пакета pkg/book: тот проверяет
только well-formedness (документ разбирается как XML), а не согласие со
схемой (документ — то, что схема считает FB2). Нашёлся случай, где первое
было true, а второе — false: xs:choice в sectionType запрещает секции с
собственным текстом одновременно быть родителем вложенных секций.

Использование: validate.py [путь к .fb2]; без аргумента читает stdin.
Код возврата: 0 — валиден, 1 — не валиден или документ не разбирается.
Ошибки схемы печатаются в stderr.
"""
import sys
from pathlib import Path

from lxml import etree


def main() -> int:
    schema_dir = Path(__file__).resolve().parent
    schema_doc = etree.parse(str(schema_dir / "FictionBook.xsd"))
    schema = etree.XMLSchema(schema_doc)

    data = Path(sys.argv[1]).read_bytes() if len(sys.argv) > 1 else sys.stdin.buffer.read()
    try:
        doc = etree.fromstring(data)
    except etree.XMLSyntaxError as e:
        print(f"XML не разбирается: {e}", file=sys.stderr)
        return 1

    if schema.validate(doc):
        return 0

    for err in schema.error_log:
        print(err, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
