-- Для сводок томов PostgreSQL может посчитать статусы по узкому индексу.
CREATE INDEX CONCURRENTLY idx_pages_work_status ON pages (work_id, status);
