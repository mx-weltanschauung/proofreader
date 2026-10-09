package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"proofreader/internal/models"
)

// workMeta несёт роль работы из тела PUT /works/{id} вместе со знанием о том,
// какие ключи клиент действительно прислал.
//
// Различие принципиально по той же причине, что и в volumeUpdate: отсутствие
// ключа обязано сохранить хранимое значение, а явный null — сбросить его.
// Форма «Edit Work» шлёт только title/author/language/country/status, и без
// этого различия сохранение из неё сняло бы работу с её тома.
type workMeta struct {
	ParentWorkID   *int64
	Role           *string
	NumberingStyle *string
	PrecedesVolume *int
	ShelfLabel     *string
	Description    *string

	HasParentWorkID   bool
	HasRole           bool
	HasNumberingStyle bool
	HasPrecedesVolume bool
	HasShelfLabel     bool
	HasDescription    bool
}

func parseWorkMeta(body []byte) (workMeta, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return workMeta{}, err
	}

	var m workMeta
	if v, ok := raw["parent_work_id"]; ok {
		if err := json.Unmarshal(v, &m.ParentWorkID); err != nil {
			return workMeta{}, fmt.Errorf("parent_work_id: %w", err)
		}
		m.HasParentWorkID = true
	}
	if v, ok := raw["role"]; ok {
		if err := json.Unmarshal(v, &m.Role); err != nil {
			return workMeta{}, fmt.Errorf("role: %w", err)
		}
		m.HasRole = true
	}
	if v, ok := raw["numbering_style"]; ok {
		if err := json.Unmarshal(v, &m.NumberingStyle); err != nil {
			return workMeta{}, fmt.Errorf("numbering_style: %w", err)
		}
		m.HasNumberingStyle = true
	}
	if v, ok := raw["precedes_volume"]; ok {
		if err := json.Unmarshal(v, &m.PrecedesVolume); err != nil {
			return workMeta{}, fmt.Errorf("precedes_volume: %w", err)
		}
		m.HasPrecedesVolume = true
	}
	if v, ok := raw["shelf_label"]; ok {
		if err := json.Unmarshal(v, &m.ShelfLabel); err != nil {
			return workMeta{}, fmt.Errorf("shelf_label: %w", err)
		}
		m.HasShelfLabel = true
	}
	if v, ok := raw["description"]; ok {
		if err := json.Unmarshal(v, &m.Description); err != nil {
			return workMeta{}, fmt.Errorf("description: %w", err)
		}
		m.HasDescription = true
	}
	return m, nil
}

var (
	validRoles = map[string]bool{
		models.WorkRoleVolume:             true,
		models.WorkRoleFrontMatter:        true,
		models.WorkRoleEditionFrontMatter: true,
		models.WorkRoleJournalIssue:       true,
	}
	validNumberings = map[string]bool{models.NumberingArabic: true, models.NumberingRoman: true}
)

// Предел подписи корешка в знаках. На корешок в 64px влезает около сотни, в
// карточку тома — примерно столько же; всё сверх этого не показывается нигде,
// зато ломает раскладку полки. Считается в рунах: 120 кириллических знаков это
// 240 байт, и байтовый предел отрезал бы русскую подпись вдвое раньше латинской.
const shelfLabelLimit = 120

// Предел описания тома в знаках. Оно не подпись корешка, а объяснение —
// почему в скане не хватает полос, откуда взят источник, — и печатается на
// карточке абзацем, а не строкой, поэтому предел заметно шире shelfLabelLimit.
// 2000 знаков — это примерно страница текста, дальше карточку тома лучше
// разгружать ссылкой на отдельный текст, а не полем формы.
const descriptionLimit = 2000

// applyWorkMeta сводит присланное с хранимым и проверяет получившуюся
// комбинацию целиком, до всякой записи — отвергнутый запрос оставляет работу
// нетронутой. Проверки повторяют works_parent_role_check, чтобы клиент видел
// 400 с объяснением, а не 500 от нарушенного констрейнта.
func applyWorkMeta(w *models.Work, m workMeta) error {
	parent := w.ParentWorkID
	if m.HasParentWorkID {
		parent = m.ParentWorkID
	}
	role := w.Role
	if m.HasRole {
		role = models.WorkRoleVolume
		if m.Role != nil {
			role = *m.Role
		}
	}
	numbering := w.NumberingStyle
	if m.HasNumberingStyle {
		numbering = models.NumberingArabic
		if m.NumberingStyle != nil {
			numbering = *m.NumberingStyle
		}
	}
	precedes := w.PrecedesVolume
	if m.HasPrecedesVolume {
		precedes = m.PrecedesVolume
	}
	shelfLabel := w.ShelfLabel
	if m.HasShelfLabel {
		// null и "" — одно и то же требование «убрать подпись», и строка из
		// одних пробелов тоже: пустое состояние должно записываться ровно
		// одним способом.
		shelfLabel = ""
		if m.ShelfLabel != nil {
			shelfLabel = strings.TrimSpace(*m.ShelfLabel)
		}
	}
	description := w.Description
	if m.HasDescription {
		// Та же логика, что у shelfLabel: null, "" и пробелы — одно состояние
		// «описания нет».
		description = ""
		if m.Description != nil {
			description = strings.TrimSpace(*m.Description)
		}
	}

	if role == "" {
		role = models.WorkRoleVolume
	}
	if numbering == "" {
		numbering = models.NumberingArabic
	}
	// Роль номера журнала ставит только POST /journals/{id}/issues — вместе со
	// строкой journal_issues. Общая правка работы её не ставит и не снимает:
	// том, ставший «номером», остался бы без строки номера (сирота нигде не
	// виден), а номер, ставший томом, при живой строке journal_issues уехал бы
	// в каталог, OPDS и карту сайта.
	stored := w.Role
	if stored == "" {
		stored = models.WorkRoleVolume
	}
	if (stored == models.WorkRoleJournalIssue) != (role == models.WorkRoleJournalIssue) {
		return fmt.Errorf("role journal_issue is set only by POST /journals/{id}/issues and cannot be changed, got %q -> %q", stored, role)
	}
	if !validRoles[role] {
		return fmt.Errorf("role must be volume, front_matter, edition_front_matter or journal_issue, got %q", role)
	}
	if !validNumberings[numbering] {
		return fmt.Errorf("numbering_style must be arabic or roman, got %q", numbering)
	}
	switch role {
	case models.WorkRoleVolume:
		if parent != nil {
			return fmt.Errorf("volume work cannot have parent_work_id")
		}
	case models.WorkRoleEditionFrontMatter:
		// Предисловие к группе томов не принадлежит тому, поэтому родителя у
		// него нет; но без издания его негде показать, а номер тома увёл бы
		// в него ссылку из предметного указателя.
		if parent != nil {
			return fmt.Errorf("edition_front_matter work cannot have parent_work_id")
		}
		if w.EditionID == nil {
			return fmt.Errorf("edition_front_matter work requires edition_id")
		}
		if w.VolumeNumber != nil {
			return fmt.Errorf("edition_front_matter work cannot have volume_number")
		}
	case models.WorkRoleJournalIssue:
		// Зеркало works_parent_role_check: номер без родителя и без издания.
		if parent != nil {
			return fmt.Errorf("journal_issue work cannot have parent_work_id")
		}
		if w.EditionID != nil || w.VolumeNumber != nil {
			return fmt.Errorf("journal_issue work cannot belong to an edition")
		}
	default:
		if parent == nil {
			return fmt.Errorf("%s work requires parent_work_id", role)
		}
	}
	if precedes != nil && role != models.WorkRoleEditionFrontMatter {
		return fmt.Errorf("precedes_volume only applies to edition_front_matter")
	}
	if precedes != nil && *precedes < 1 {
		return fmt.Errorf("precedes_volume must be positive, got %d", *precedes)
	}
	if n := len([]rune(shelfLabel)); n > shelfLabelLimit {
		return fmt.Errorf("shelf_label must be at most %d characters, got %d", shelfLabelLimit, n)
	}
	if n := len([]rune(description)); n > descriptionLimit {
		return fmt.Errorf("description must be at most %d characters, got %d", descriptionLimit, n)
	}
	if parent != nil && *parent == w.ID {
		return fmt.Errorf("work cannot be its own parent")
	}

	w.ParentWorkID = parent
	w.Role = role
	w.NumberingStyle = numbering
	w.PrecedesVolume = precedes
	w.ShelfLabel = shelfLabel
	w.Description = description
	return nil
}
