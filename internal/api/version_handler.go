package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// SchemaVersionFunc сообщает применённую версию миграций и признак «грязно».
// main.go замыкает её над пулом pgx, тесты подставляют свою.
type SchemaVersionFunc func(context.Context) (int, bool, error)

// VersionHandler отвечает на GET /api/version. Его читают оба канала доставки:
// scripts/release.sh убеждается, что выкатка доехала, а publish_volume.py
// отказывается публиковать в базу со схемой ниже локальной.
type VersionHandler struct {
	schemaVersion SchemaVersionFunc
	commit        string
}

func NewVersionHandler(schema SchemaVersionFunc, commit string) *VersionHandler {
	return &VersionHandler{schemaVersion: schema, commit: commit}
}

type versionResponse struct {
	SchemaVersion int    `json:"schema_version"`
	SchemaDirty   bool   `json:"schema_dirty"`
	Commit        string `json:"commit"`
}

// Version — публичный маршрут: он не раскрывает ничего, чего не видно по
// поведению API, а нужен инструментам, которые ходят до входа в систему.
func (h *VersionHandler) Version(w http.ResponseWriter, r *http.Request) {
	version, dirty, err := h.schemaVersion(r.Context())
	if err != nil {
		http.Error(w, "Failed to read schema version", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versionResponse{
		SchemaVersion: version,
		SchemaDirty:   dirty,
		Commit:        h.commit,
	})
}
