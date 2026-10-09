package api

import (
	"strings"
	"testing"

	"proofreader/internal/models"
)

func frontMatterWork() *models.Work {
	parent := int64(41)
	return &models.Work{
		ID: 42, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, ParentWorkID: &parent,
	}
}

// Тело без наших ключей (то, что шлёт форма «Edit Work») не должно
// обнулять роль, стиль и связь с томом.
func TestApplyWorkMeta_AbsentKeysPreserve(t *testing.T) {
	w := frontMatterWork()
	m, err := parseWorkMeta([]byte(`{"title":"Новое имя","status":"draft"}`))
	if err != nil {
		t.Fatalf("parseWorkMeta: %v", err)
	}
	if err := applyWorkMeta(w, m); err != nil {
		t.Fatalf("applyWorkMeta: %v", err)
	}
	if w.Role != models.WorkRoleFrontMatter {
		t.Errorf("Role = %q, want %q", w.Role, models.WorkRoleFrontMatter)
	}
	if w.NumberingStyle != models.NumberingRoman {
		t.Errorf("NumberingStyle = %q, want %q", w.NumberingStyle, models.NumberingRoman)
	}
	if w.ParentWorkID == nil || *w.ParentWorkID != 41 {
		t.Errorf("ParentWorkID = %v, want 41", w.ParentWorkID)
	}
}

func TestApplyWorkMeta_SetsRoleAndParentTogether(t *testing.T) {
	w := &models.Work{ID: 42, Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic}
	m, err := parseWorkMeta([]byte(
		`{"role":"front_matter","parent_work_id":41,"numbering_style":"roman"}`))
	if err != nil {
		t.Fatalf("parseWorkMeta: %v", err)
	}
	if err := applyWorkMeta(w, m); err != nil {
		t.Fatalf("applyWorkMeta: %v", err)
	}
	if w.Role != models.WorkRoleFrontMatter || w.NumberingStyle != models.NumberingRoman {
		t.Fatalf("роль/стиль не применились: %+v", w)
	}
	if w.ParentWorkID == nil || *w.ParentWorkID != 41 {
		t.Fatalf("ParentWorkID = %v, want 41", w.ParentWorkID)
	}
}

// Комбинации, которые отвергнёт CHECK в базе, должны отвергаться раньше —
// с внятным 400 вместо 500 от Postgres.
func TestApplyWorkMeta_RejectsBadCombinations(t *testing.T) {
	cases := map[string]string{
		"служебная без родителя": `{"role":"front_matter"}`,
		"том с родителем":        `{"role":"volume","parent_work_id":41}`,
		"неизвестная роль":       `{"role":"appendix","parent_work_id":41}`,
		"неизвестный стиль":      `{"numbering_style":"greek"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			w := &models.Work{ID: 42, Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic}
			m, err := parseWorkMeta([]byte(body))
			if err != nil {
				return // разбор тоже вправе отвергнуть
			}
			if err := applyWorkMeta(w, m); err == nil {
				t.Fatalf("применение %s прошло, ожидалась ошибка", body)
			}
		})
	}
}

// Работа не может быть собственным родителем.
func TestApplyWorkMeta_RejectsSelfParent(t *testing.T) {
	w := &models.Work{ID: 42, Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic}
	m, err := parseWorkMeta([]byte(`{"role":"front_matter","parent_work_id":42}`))
	if err != nil {
		t.Fatalf("parseWorkMeta: %v", err)
	}
	if err := applyWorkMeta(w, m); err == nil {
		t.Fatal("работа не может быть собственным родителем")
	}
}

func editionWork() *models.Work {
	edition := int64(1)
	return &models.Work{ID: 7, EditionID: &edition, Role: models.WorkRoleVolume}
}

func TestApplyWorkMeta_AcceptsEditionFrontMatter(t *testing.T) {
	w := editionWork()
	role := models.WorkRoleEditionFrontMatter
	precedes := 1
	err := applyWorkMeta(w, workMeta{
		Role: &role, HasRole: true,
		PrecedesVolume: &precedes, HasPrecedesVolume: true,
	})
	if err != nil {
		t.Fatalf("ожидалось принятие, получено: %v", err)
	}
	if w.PrecedesVolume == nil || *w.PrecedesVolume != 1 {
		t.Fatalf("precedes_volume не записан: %v", w.PrecedesVolume)
	}
}

func TestApplyWorkMeta_RejectsEditionFrontMatterWithoutEdition(t *testing.T) {
	w := editionWork()
	w.EditionID = nil
	role := models.WorkRoleEditionFrontMatter
	err := applyWorkMeta(w, workMeta{Role: &role, HasRole: true})
	if err == nil || !strings.Contains(err.Error(), "edition_id") {
		t.Fatalf("ожидался отказ про edition_id, получено: %v", err)
	}
}

func TestApplyWorkMeta_RejectsEditionFrontMatterWithParent(t *testing.T) {
	w := editionWork()
	role := models.WorkRoleEditionFrontMatter
	parent := int64(3)
	err := applyWorkMeta(w, workMeta{
		Role: &role, HasRole: true,
		ParentWorkID: &parent, HasParentWorkID: true,
	})
	if err == nil || !strings.Contains(err.Error(), "parent_work_id") {
		t.Fatalf("ожидался отказ про parent_work_id, получено: %v", err)
	}
}

func TestApplyWorkMeta_RejectsEditionFrontMatterWithVolumeNumber(t *testing.T) {
	w := editionWork()
	number := 1
	w.VolumeNumber = &number
	role := models.WorkRoleEditionFrontMatter
	err := applyWorkMeta(w, workMeta{Role: &role, HasRole: true})
	if err == nil || !strings.Contains(err.Error(), "volume_number") {
		t.Fatalf("ожидался отказ про volume_number, получено: %v", err)
	}
}

func TestApplyWorkMeta_RejectsPrecedesVolumeOnAVolume(t *testing.T) {
	w := editionWork()
	precedes := 1
	err := applyWorkMeta(w, workMeta{
		PrecedesVolume: &precedes, HasPrecedesVolume: true,
	})
	if err == nil || !strings.Contains(err.Error(), "precedes_volume") {
		t.Fatalf("ожидался отказ про precedes_volume, получено: %v", err)
	}
}

func TestApplyWorkMeta_RejectsNonPositivePrecedesVolume(t *testing.T) {
	cases := map[string]int{
		"zero":     0,
		"negative": -1,
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			w := editionWork()
			role := models.WorkRoleEditionFrontMatter
			precedes := val
			err := applyWorkMeta(w, workMeta{
				Role: &role, HasRole: true,
				PrecedesVolume: &precedes, HasPrecedesVolume: true,
			})
			if err == nil || !strings.Contains(err.Error(), "precedes_volume") {
				t.Fatalf("ожидался отказ про precedes_volume для %d, получено: %v", val, err)
			}
		})
	}
}

func volumeWork(label string) *models.Work {
	return &models.Work{
		ID: 7, Role: models.WorkRoleVolume,
		NumberingStyle: models.NumberingArabic, ShelfLabel: label,
	}
}

// Форма «Edit Work» шлёт только title/author/language/country/status. Если бы
// отсутствие ключа не отличалось от null, сохранение из неё стирало бы подпись.
func TestApplyWorkMeta_KeepsShelfLabelWhenKeyAbsent(t *testing.T) {
	w := volumeWork("1893—1894")
	m, err := parseWorkMeta([]byte(`{"title":"Том 1"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyWorkMeta(w, m); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if w.ShelfLabel != "1893—1894" {
		t.Errorf("ShelfLabel = %q, ожидалось сохранённое %q", w.ShelfLabel, "1893—1894")
	}
}

func TestApplyWorkMeta_SetsAndClearsShelfLabel(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"строка ставит", `{"shelf_label":"Материализм и эмпириокритицизм"}`, "Материализм и эмпириокритицизм"},
		{"null сбрасывает", `{"shelf_label":null}`, ""},
		{"пустая строка сбрасывает", `{"shelf_label":""}`, ""},
		{"пробелы сбрасывают", `{"shelf_label":"   "}`, ""},
		{"пробелы по краям срезаются", `{"shelf_label":"  Что делать?  "}`, "Что делать?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := volumeWork("прежняя")
			m, err := parseWorkMeta([]byte(c.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := applyWorkMeta(w, m); err != nil {
				t.Fatalf("apply: %v", err)
			}
			if w.ShelfLabel != c.want {
				t.Errorf("ShelfLabel = %q, ожидалось %q", w.ShelfLabel, c.want)
			}
		})
	}
}

// Длина считается в рунах, а не в байтах: 120 кириллических знаков это 240
// байт, и байтовый предел отрезал бы русскую подпись вдвое раньше латинской.
func TestApplyWorkMeta_RejectsOverlongShelfLabel(t *testing.T) {
	long := ""
	for i := 0; i < 121; i++ {
		long += "я"
	}
	w := volumeWork("")
	m, err := parseWorkMeta([]byte(`{"shelf_label":"` + long + `"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyWorkMeta(w, m); err == nil {
		t.Fatal("подпись в 121 знак принята, ожидалась ошибка")
	}
	if w.ShelfLabel != "" {
		t.Errorf("отвергнутый запрос изменил работу: ShelfLabel = %q", w.ShelfLabel)
	}
}

// 120 знаков — граница, и она входит в допустимое.
func TestApplyWorkMeta_AcceptsShelfLabelAtLimit(t *testing.T) {
	exact := ""
	for i := 0; i < 120; i++ {
		exact += "я"
	}
	w := volumeWork("")
	m, err := parseWorkMeta([]byte(`{"shelf_label":"` + exact + `"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyWorkMeta(w, m); err != nil {
		t.Fatalf("подпись ровно в 120 знаков отвергнута: %v", err)
	}
	if w.ShelfLabel != exact {
		t.Errorf("подпись не записалась целиком: %d знаков", len([]rune(w.ShelfLabel)))
	}
}

func descriptionWork(desc string) *models.Work {
	return &models.Work{
		ID: 7, Role: models.WorkRoleVolume,
		NumberingStyle: models.NumberingArabic, Description: desc,
	}
}

// Форма «Edit Work» шлёт только title/author/language/country/status. Если бы
// отсутствие ключа не отличалось от null, сохранение из неё стирало бы
// описание тома при каждой правке заголовка.
func TestApplyWorkMeta_KeepsDescriptionWhenKeyAbsent(t *testing.T) {
	w := descriptionWork("скан утерян начиная со стр. 200")
	m, err := parseWorkMeta([]byte(`{"title":"Том 1"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyWorkMeta(w, m); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if w.Description != "скан утерян начиная со стр. 200" {
		t.Errorf("Description = %q, ожидалось сохранённое", w.Description)
	}
}

func TestApplyWorkMeta_SetsAndClearsDescription(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"строка ставит", `{"description":"В книге недостаёт нескольких страниц"}`, "В книге недостаёт нескольких страниц"},
		{"null сбрасывает", `{"description":null}`, ""},
		{"пустая строка сбрасывает", `{"description":""}`, ""},
		{"пробелы сбрасывают", `{"description":"   "}`, ""},
		{"пробелы по краям срезаются", `{"description":"  Реставрация переплёта  "}`, "Реставрация переплёта"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := descriptionWork("прежнее описание")
			m, err := parseWorkMeta([]byte(c.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := applyWorkMeta(w, m); err != nil {
				t.Fatalf("apply: %v", err)
			}
			if w.Description != c.want {
				t.Errorf("Description = %q, ожидалось %q", w.Description, c.want)
			}
		})
	}
}

// Предел в 2000 знаков — отказ, и отвергнутый запрос не должен трогать
// хранимое описание.
func TestApplyWorkMeta_RejectsOverlongDescription(t *testing.T) {
	long := strings.Repeat("я", descriptionLimit+1)
	w := descriptionWork("")
	m, err := parseWorkMeta([]byte(`{"description":"` + long + `"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyWorkMeta(w, m); err == nil {
		t.Fatal("описание длиннее предела принято, ожидалась ошибка")
	}
	if w.Description != "" {
		t.Errorf("отвергнутый запрос изменил работу: Description = %q", w.Description)
	}
}

func TestApplyWorkMeta_JournalIssueRole(t *testing.T) {
	role := models.WorkRoleJournalIssue
	meta := workMeta{Role: &role, HasRole: true}

	// Зеркало works_parent_role_check у самого номера.
	parent := int64(7)
	w := &models.Work{ID: 42, Role: models.WorkRoleJournalIssue}
	if err := applyWorkMeta(w, workMeta{Role: &role, HasRole: true, ParentWorkID: &parent, HasParentWorkID: true}); err == nil {
		t.Fatal("journal_issue с parent_work_id принят")
	}
	ed := int64(3)
	w = &models.Work{ID: 42, Role: models.WorkRoleJournalIssue, EditionID: &ed}
	if err := applyWorkMeta(w, meta); err == nil {
		t.Fatal("journal_issue с edition_id принят")
	}
	w = &models.Work{ID: 42, Role: models.WorkRoleJournalIssue}
	if err := applyWorkMeta(w, meta); err != nil {
		t.Fatalf("номер с той же ролью отвергнут: %v", err)
	}
	// Правка номера без ключа роли роль не трогает.
	if err := applyWorkMeta(w, workMeta{}); err != nil || w.Role != models.WorkRoleJournalIssue {
		t.Fatalf("правка номера без роли: %v, роль %q", err, w.Role)
	}
}

// Финальная рецензия: роль номера ставит и снимает только создание номера.
func TestApplyWorkMeta_JournalIssueRoleCannotChange(t *testing.T) {
	issue := models.WorkRoleJournalIssue
	volume := models.WorkRoleVolume
	for _, tc := range []struct {
		name   string
		stored string
		meta   workMeta
	}{
		{"том -> номер", models.WorkRoleVolume, workMeta{Role: &issue, HasRole: true}},
		{"работа без роли -> номер", "", workMeta{Role: &issue, HasRole: true}},
		{"номер -> том", models.WorkRoleJournalIssue, workMeta{Role: &volume, HasRole: true}},
		{"номер -> null (том)", models.WorkRoleJournalIssue, workMeta{HasRole: true}},
	} {
		w := &models.Work{ID: 42, Role: tc.stored}
		err := applyWorkMeta(w, tc.meta)
		if err == nil || !strings.Contains(err.Error(), "journal_issue") {
			t.Errorf("%s: принято (%v)", tc.name, err)
		}
		if w.Role != tc.stored {
			t.Errorf("%s: отвергнутый запрос изменил роль: %q", tc.name, w.Role)
		}
	}
}
