-- Сносит таблицу вместе с ручным выбором избранного; вернуть его можно только
-- повторным apply_curation.py (tools/ocr_ingest/shelf_curation.json).
DROP TABLE IF EXISTS edition_highlights;
