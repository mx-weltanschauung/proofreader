-- Сносит все строки звука; объекты в бакете S3_AUDIO_BUCKET остаются
-- сиротами — перед откатом на боевом их список снимается `mc ls`.
DROP TABLE IF EXISTS audio_recordings;
DROP TABLE IF EXISTS audio_tracks;
DROP TABLE IF EXISTS audio_queue;
