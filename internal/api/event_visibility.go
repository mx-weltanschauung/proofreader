package api

import "proofreader/internal/models"

// visibleEvent — событие глазами зрителя; всегда копия. Не-сотруднику цель
// битая или в закрытой работе выбрасывается, строка без целей — тоже, пункт
// без строк — тоже; ни подпись строки, ни заглавие пункта не остаются, когда
// за ними нечего показать. false — не осталось ни одного пункта. Позиции
// уцелевших пунктов не пересчитываются: якорь из ссылки главы не сдвигается.
// Сотрудник видит всё, закрытые цели помечены. Цель-глава из closedChapters
// ведёт себя как цель в закрытой работе.
func visibleEvent(d *models.EventDetail, closed, closedChapters ClosedSet, staff bool) (*models.EventDetail, bool) {
	out := *d
	out.Sections = make([]models.EventSection, 0, len(d.Sections))
	for _, s := range d.Sections {
		sec := models.EventSection{Position: s.Position, Title: s.Title,
			Entries: make([]models.EventEntry, 0, len(s.Entries))}
		for _, e := range s.Entries {
			entry := models.EventEntry{Label: e.Label, Targets: make([]models.EventTarget, 0, len(e.Targets))}
			for _, t := range e.Targets {
				isClosed := (t.WorkID != nil && closed.Has(*t.WorkID)) ||
					(t.ChapterID != nil && closedChapters.Has(*t.ChapterID))
				if !staff && (t.Broken || isClosed) {
					continue
				}
				if staff {
					t.Closed = isClosed
				}
				entry.Targets = append(entry.Targets, t)
			}
			if staff || len(entry.Targets) > 0 {
				sec.Entries = append(sec.Entries, entry)
			}
		}
		if staff || len(sec.Entries) > 0 {
			out.Sections = append(out.Sections, sec)
		}
	}
	if staff {
		return &out, true
	}
	return &out, len(out.Sections) > 0
}

// visibleOrganization — организация глазами зрителя; всегда копия.
// MaterialsTotal считается по целям в открытых работах (сотруднику — по всем).
// Не-сотруднику событие без открытых материалов и вид без событий
// выбрасываются; false — не осталось ни одного события.
func visibleOrganization(o *models.EventOrganizationDetail, closed, closedChapters ClosedSet, staff bool) (*models.EventOrganizationDetail, bool) {
	out := *o
	out.Kinds = make([]models.EventKindEvents, 0, len(o.Kinds))
	total := 0
	for _, k := range o.Kinds {
		kind := models.EventKindEvents{EventKind: k.EventKind, Events: make([]models.EventSummary, 0, len(k.Events))}
		for _, e := range k.Events {
			n := 0
			for i, id := range e.TargetWorkIDs {
				chapterID := int64(0)
				if i < len(e.TargetChapterIDs) {
					chapterID = e.TargetChapterIDs[i]
				}
				if staff || (!closed.Has(id) && !closedChapters.Has(chapterID)) {
					n++
				}
			}
			if staff {
				n = len(e.TargetWorkIDs)
			}
			if !staff && n == 0 {
				continue
			}
			e.MaterialsTotal = n
			kind.Events = append(kind.Events, e)
		}
		if staff || len(kind.Events) > 0 {
			out.Kinds = append(out.Kinds, kind)
			total += len(kind.Events)
		}
	}
	if staff {
		return &out, true
	}
	return &out, total > 0
}
