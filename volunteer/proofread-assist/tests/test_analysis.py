from proofread_assist.analysis import candidates
from proofread_assist.ocr import ocr_candidates


def test_occurrence_offsets_are_exact():
    row = dict(old="мир", new="мip", reason="проверить")
    result = candidates("мир мир", [row, dict(row, start_rune=4, end_rune=7)])
    assert [(f["old"], f["start_rune"], f["end_rune"]) for f in result] == [("мир", 4, 7)]
    assert candidates("😀мир", [dict(row, start_rune=2, end_rune=5)]) == []


def test_upload_idempotent():
    row = dict(old="я", new="Я", reason="регистр", start_rune=0, end_rune=1)
    first = candidates("я", [row, row])
    second = candidates("я", [dict(row, reason="начало")])
    assert len(first) == 1
    assert first[0]["candidate_key"] == second[0]["candidate_key"]


def test_without_tesseract(monkeypatch):
    monkeypatch.setenv("PATH", "")
    assert ocr_candidates("текст", b"scan") == []


def test_ocr_finds_word_between_unique_neighbours():
    from proofread_assist.ocr import from_tsv

    tsv = "level\ttext\twidth\theight\tleft\ttop\tconf\n"
    tsv += "1\t\t100\t200\t0\t0\t-1\n"
    for word in ("До", "войны", "здесь"):
        tsv += f"5\t{word}\t20\t10\t10\t20\t95\n"
    result = from_tsv("До воины здесь", tsv)
    assert len(result) == 1
    assert result[0]["source"] == "ocr"
    assert result[0]["scan_bbox"] == [0.1, 0.1, 0.2, 0.05]
    assert result[0]["start_rune"] == 3


def test_text_only_model_supported(serve):
    from proofread_assist.model import analyze
    import json

    def respond(method, path, headers, body):
        assert body["messages"][-1]["content"] == "😀 мир мир"
        row = dict(old="мир", new="мiр", reason="проверить", start_rune=6, end_rune=9)
        return 200, {"choices": [{"message": {"content": json.dumps([row])}}]}

    result = analyze(serve(respond), "model", None, "😀 мир мир")
    assert result[0]["start_rune"] == 6
    assert result[0]["source"] == "model"
