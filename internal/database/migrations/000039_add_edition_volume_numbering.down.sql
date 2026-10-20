-- Снимает признак условной нумерации. Данные томов не трогаются: номера
-- остаются на месте, но собрание с 'order' после отката снова печатает их
-- читателю как «т. N» — вернуть признак можно только повторной правкой
-- собрания (PUT /api/editions/{id}, volume_numbering = "order").
ALTER TABLE editions DROP COLUMN IF EXISTS volume_numbering;
