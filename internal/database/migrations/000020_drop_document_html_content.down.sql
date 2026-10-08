-- Колонка возвращается пустой: содержимое не восстанавливается, да и
-- восстанавливать было нечего — на момент сноса в documents ноль строк.
ALTER TABLE documents ADD COLUMN html_content TEXT;
