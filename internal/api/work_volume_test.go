package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgconn"

	"proofreader/internal/models"
)

func ptrInt(v int) *int       { return &v }
func ptrStr(v string) *string { return &v }

// mustParseVolumeUpdate parses a JSON body the way the handler does.
func mustParseVolumeUpdate(t *testing.T, body string) volumeUpdate {
	t.Helper()
	u, err := parseVolumeUpdate([]byte(body))
	if err != nil {
		t.Fatalf("parseVolumeUpdate(%s): %v", body, err)
	}
	return u
}

func TestApplyVolumeFields(t *testing.T) {
	tests := []struct {
		name    string
		before  models.Work // состояние работы до запроса
		body    string
		wantErr bool

		wantEdition *int64
		wantNumber  *int
		wantPart    *string
		wantOff     int
	}{
		{
			name:        "полутом I у тома 25",
			body:        `{"edition_id":3,"volume_number":25,"volume_part":"I","page_offset":0}`,
			wantEdition: ptrInt64(3),
			wantNumber:  ptrInt(25),
			wantPart:    ptrStr("I"),
		},
		{
			name:    "полутом у тома без полутомов",
			body:    `{"volume_number":24,"volume_part":"I"}`,
			wantErr: true,
		},
		{
			name:        "полутом II у тома 46",
			body:        `{"edition_id":1,"volume_number":46,"volume_part":"II","page_offset":0}`,
			wantEdition: ptrInt64(1),
			wantNumber:  ptrInt(46),
			wantPart:    ptrStr("II"),
		},
		{
			name:    "полутома III у тома 46 нет",
			body:    `{"volume_number":46,"volume_part":"III"}`,
			wantErr: true,
		},
		{
			name:    "полутом IV не существует",
			body:    `{"volume_number":26,"volume_part":"IV"}`,
			wantErr: true,
		},
		{
			name:    "полутом без номера тома",
			body:    `{"volume_part":"II"}`,
			wantErr: true,
		},
		{
			name:    "нулевой номер тома",
			body:    `{"volume_number":0}`,
			wantErr: true,
		},
		{
			name:       "отрицательный оффсет допустим",
			body:       `{"volume_number":1,"page_offset":-8}`,
			wantNumber: ptrInt(1),
			wantOff:    -8,
		},
		{
			// главная находка C2: форма «Edit Work» шлёт только текстовые поля.
			name: "ключей нет — координаты тома сохраняются",
			before: models.Work{
				EditionID: ptrInt64(3), VolumeNumber: ptrInt(26),
				VolumePart: ptrStr("II"), PageOffset: -8,
			},
			body:        `{"title":"Сочинения","status":"in_progress"}`,
			wantEdition: ptrInt64(3),
			wantNumber:  ptrInt(26),
			wantPart:    ptrStr("II"),
			wantOff:     -8,
		},
		{
			name: "явный null сбрасывает",
			before: models.Work{
				EditionID: ptrInt64(3), VolumeNumber: ptrInt(26),
				VolumePart: ptrStr("II"), PageOffset: -8,
			},
			body: `{"edition_id":null,"volume_number":null,"volume_part":null,"page_offset":null}`,
		},
		{
			name: "пустая строка сбрасывает полутом",
			before: models.Work{
				EditionID: ptrInt64(3), VolumeNumber: ptrInt(26), VolumePart: ptrStr("II"),
			},
			body:        `{"volume_part":""}`,
			wantEdition: ptrInt64(3),
			wantNumber:  ptrInt(26),
		},
		{
			name: "валидное значение записывается поверх прежнего",
			before: models.Work{
				EditionID: ptrInt64(3), VolumeNumber: ptrInt(26), VolumePart: ptrStr("II"),
			},
			body:        `{"volume_part":"III"}`,
			wantEdition: ptrInt64(3),
			wantNumber:  ptrInt(26),
			wantPart:    ptrStr("III"),
		},
		{
			// сохранённый полутом проверяется против нового номера тома —
			// молча его не роняем.
			name: "смена тома под сохранённым полутомом отвергается",
			before: models.Work{
				EditionID: ptrInt64(3), VolumeNumber: ptrInt(26), VolumePart: ptrStr("III"),
			},
			body:    `{"volume_number":24}`,
			wantErr: true,
		},
		{
			name:    "полутом строкой с мусором",
			body:    `{"volume_number":26,"volume_part":"IX"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tt.before
			err := applyVolumeFields(&w, mustParseVolumeUpdate(t, tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, получено nil")
				}
				// работа не должна быть тронута отвергнутым запросом
				if !sameVolume(w, tt.before) {
					t.Fatalf("отвергнутый запрос изменил работу: %+v", w)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if !eqInt64Ptr(w.EditionID, tt.wantEdition) {
				t.Errorf("EditionID = %v, ожидался %v", fmtInt64Ptr(w.EditionID), fmtInt64Ptr(tt.wantEdition))
			}
			if !eqIntPtr(w.VolumeNumber, tt.wantNumber) {
				t.Errorf("VolumeNumber = %v, ожидался %v", fmtIntPtr(w.VolumeNumber), fmtIntPtr(tt.wantNumber))
			}
			if !eqStrPtr(w.VolumePart, tt.wantPart) {
				t.Errorf("VolumePart = %v, ожидался %v", fmtStrPtr(w.VolumePart), fmtStrPtr(tt.wantPart))
			}
			if w.PageOffset != tt.wantOff {
				t.Errorf("PageOffset = %d, ожидался %d", w.PageOffset, tt.wantOff)
			}
		})
	}
}

func TestParseVolumeUpdatePresence(t *testing.T) {
	u := mustParseVolumeUpdate(t, `{"title":"x"}`)
	if u.HasEditionID || u.HasVolumeNumber || u.HasVolumePart || u.HasPageOffset {
		t.Fatalf("ключей в теле нет, а признаки присутствия выставлены: %+v", u)
	}

	u = mustParseVolumeUpdate(t, `{"volume_number":null,"page_offset":0}`)
	if !u.HasVolumeNumber || u.VolumeNumber != nil {
		t.Errorf("volume_number: null должен давать присутствие с nil, получено %+v", u)
	}
	if !u.HasPageOffset || u.PageOffset == nil || *u.PageOffset != 0 {
		t.Errorf("page_offset: 0 должен давать присутствие со значением 0, получено %+v", u)
	}

	if _, err := parseVolumeUpdate([]byte(`{"volume_number":"двадцать"}`)); err == nil {
		t.Error("нечисловой volume_number должен давать ошибку разбора")
	}
}

func TestIsVolumeTakenError(t *testing.T) {
	taken := &pgconn.PgError{Code: "23505", ConstraintName: "idx_works_edition_volume"}
	// репозиторий заворачивает ошибку через %w — распознавать надо и обёрнутую
	if !isVolumeTakenError(fmt.Errorf("failed to update work: %w", taken)) {
		t.Error("нарушение idx_works_edition_volume должно распознаваться")
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "works_slug_key"}
	if isVolumeTakenError(other) {
		t.Error("чужой уникальный индекс не должен считаться конфликтом тома")
	}
	if isVolumeTakenError(&pgconn.PgError{Code: "23503", ConstraintName: "idx_works_edition_volume"}) {
		t.Error("не-23505 не должен считаться конфликтом тома")
	}
	if isVolumeTakenError(errors.New("boom")) {
		t.Error("обычная ошибка не должна считаться конфликтом тома")
	}
}

// TestWorkHandlerUpdateInvalidBody проверяет уровень хендлера без базы:
// тело разбирается до всякого обращения к репозиторию, поэтому невалидный
// JSON отбивается на nil-репозитории. Кейсы «ключа нет» и «конфликт тома»
// требуют базы и покрыты на уровне applyVolumeFields/isVolumeTakenError.
func TestWorkHandlerUpdateInvalidBody(t *testing.T) {
	h := &WorkHandler{}
	req := httptest.NewRequest(http.MethodPut, "/api/works/1", strings.NewReader(`{`))
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func eqInt64Ptr(a, b *int64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
func eqIntPtr(a, b *int) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func eqStrPtr(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
func fmtInt64Ptr(p *int64) string {
	if p == nil {
		return "nil"
	}
	b, _ := json.Marshal(*p)
	return string(b)
}
func fmtIntPtr(p *int) string {
	if p == nil {
		return "nil"
	}
	b, _ := json.Marshal(*p)
	return string(b)
}
func fmtStrPtr(p *string) string {
	if p == nil {
		return "nil"
	}
	return *p
}

func sameVolume(a, b models.Work) bool {
	return eqInt64Ptr(a.EditionID, b.EditionID) &&
		eqIntPtr(a.VolumeNumber, b.VolumeNumber) &&
		eqStrPtr(a.VolumePart, b.VolumePart) &&
		a.PageOffset == b.PageOffset
}

// ptrInt64 живёт здесь, а не в общем хелпере: остальные тесты пакета его не используют.
func ptrInt64(v int64) *int64 { return &v }
