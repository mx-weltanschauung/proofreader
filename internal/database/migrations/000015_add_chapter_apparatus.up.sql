-- Аппарат тома (примечания, указатели, списки, приложения) перестаёт быть
-- предикатом по заголовку внутри одного запроса и становится свойством главы.
-- Классифицирует models.IsApparatusTitle при создании главы; здесь — разовая
-- простановка задним числом по тому же правилу.
ALTER TABLE chapters ADD COLUMN is_apparatus boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN chapters.is_apparatus IS
    'Глава — часть аппарата тома, а не произведение. Ставится классификатором при создании, правится руками.';

-- Подглава наследует признак родителя: раздел внутри «Примечаний» — тоже
-- аппарат, даже если сам называется нейтрально.
WITH RECURSIVE by_title AS (
    SELECT id, parent_id,
           lower(title) LIKE ANY (ARRAY[
               'примечани%', 'указател%', 'список%', 'даты жизни%', 'приложени%',
               'содержание%', 'подготовительные материалы%', 'тематический указател%'
           ]) AS own_match
    FROM chapters
),
tree AS (
    SELECT id, own_match AS apparatus FROM by_title WHERE parent_id IS NULL
    UNION ALL
    SELECT c.id, t.apparatus OR c.own_match
    FROM by_title c JOIN tree t ON c.parent_id = t.id
)
UPDATE chapters SET is_apparatus = true
WHERE id IN (SELECT id FROM tree WHERE apparatus);
