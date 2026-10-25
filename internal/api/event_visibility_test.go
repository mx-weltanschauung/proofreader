package api

import (
	"encoding/json"
	"strings"
	"testing"

	"proofreader/internal/models"
)

func tgt(workID int64, label string) models.EventTarget {
	w := workID
	return models.EventTarget{Kind: models.EventTargetWork, WorkID: &w, Label: label, Title: "Заглавие"}
}

func visibilityFixture() *models.EventDetail {
	return &models.EventDetail{
		Event: models.Event{ID: 1, Slug: "i-sezd", Title: "I съезд"},
		Sections: []models.EventSection{
			{Position: 0, Title: "Открытый пункт", Entries: []models.EventEntry{
				{Label: "Открытый доклад", Targets: []models.EventTarget{tgt(1, ""), tgt(2, "")}},
				{Label: "Секретный доклад", Targets: []models.EventTarget{tgt(2, "")}},
			}},
			{Position: 1, Title: "Секретный пункт", Entries: []models.EventEntry{
				{Label: "Другой секретный", Targets: []models.EventTarget{tgt(2, "")}},
			}},
		},
	}
}

func TestVisibleEventDropsClosedTargetsAndEmptyEntries(t *testing.T) {
	src := visibilityFixture()
	got, ok := visibleEvent(src, ClosedSet{2: true}, nil, false)
	if !ok {
		t.Fatal("событие с открытой целью исчезло")
	}
	if len(got.Sections) != 1 || len(got.Sections[0].Entries) != 1 || len(got.Sections[0].Entries[0].Targets) != 1 {
		t.Fatalf("осталось не то: %+v", got.Sections)
	}
	if got.Sections[0].Position != 0 {
		t.Errorf("позиция пункта сдвинута: %d", got.Sections[0].Position)
	}
	raw, _ := json.Marshal(got)
	for _, leak := range []string{"Секретный доклад", "Секретный пункт", "Другой секретный"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("утечка %q: %s", leak, raw)
		}
	}
	// исходное значение не тронуто
	if len(src.Sections) != 2 || len(src.Sections[0].Entries) != 2 || len(src.Sections[0].Entries[0].Targets) != 2 {
		t.Error("visibleEvent изменил переданное значение")
	}
}

func TestVisibleEventKeepsPositionOfLaterSection(t *testing.T) {
	src := visibilityFixture()
	got, ok := visibleEvent(src, ClosedSet{1: true}, nil, false)
	if !ok || len(got.Sections) != 2 {
		t.Fatalf("ok=%v %+v", ok, got)
	}
	if got.Sections[0].Position != 0 || got.Sections[1].Position != 1 {
		t.Error("позиции сбились")
	}
	_, ok = visibleEvent(src, ClosedSet{1: true, 2: true}, nil, false)
	if ok {
		t.Error("всё закрыто, а событие есть")
	}
}

func TestVisibleEventAllClosedIsGone(t *testing.T) {
	_, ok := visibleEvent(visibilityFixture(), ClosedSet{1: true, 2: true}, nil, false)
	if ok {
		t.Fatal("всё закрыто, а событие видно")
	}
}

func TestVisibleEventHidesBrokenFromOutsiders(t *testing.T) {
	d := &models.EventDetail{Sections: []models.EventSection{{Title: "П", Entries: []models.EventEntry{
		{Label: "С битой", Targets: []models.EventTarget{{Kind: "chapter", Broken: true, Label: "снимок"}, tgt(1, "")}},
	}}}}
	got, ok := visibleEvent(d, nil, nil, false)
	if !ok || len(got.Sections[0].Entries[0].Targets) != 1 {
		t.Fatalf("битая цель не выброшена: %+v", got)
	}
	got, ok = visibleEvent(d, nil, nil, true)
	if !ok || len(got.Sections[0].Entries[0].Targets) != 2 || !got.Sections[0].Entries[0].Targets[0].Broken {
		t.Fatalf("сотруднику битая цель пропала: %+v", got)
	}
}

func TestVisibleEventMarksClosedForStaff(t *testing.T) {
	got, ok := visibleEvent(visibilityFixture(), ClosedSet{2: true}, nil, true)
	if !ok || len(got.Sections) != 2 || len(got.Sections[0].Entries) != 2 {
		t.Fatalf("сотруднику выброшено: %+v", got)
	}
	e := got.Sections[0].Entries[0]
	if e.Targets[0].Closed || !e.Targets[1].Closed {
		t.Errorf("пометка Closed неверна: %+v", e.Targets)
	}
}

func TestVisibleOrganizationCountsOpenMaterials(t *testing.T) {
	org := &models.EventOrganizationDetail{Kinds: []models.EventKindEvents{
		{EventKind: models.EventKind{Title: "Съезды"}, Events: []models.EventSummary{
			{Slug: "a", MaterialsTotal: 3, TargetWorkIDs: []int64{1, 1, 2}},
			{Slug: "b", MaterialsTotal: 1, TargetWorkIDs: []int64{2}},
		}},
		{EventKind: models.EventKind{Title: "Пленумы"}, Events: []models.EventSummary{
			{Slug: "c", MaterialsTotal: 1, TargetWorkIDs: []int64{2}},
		}},
	}}
	got, ok := visibleOrganization(org, ClosedSet{2: true}, nil, false)
	if !ok || len(got.Kinds) != 1 || len(got.Kinds[0].Events) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Kinds[0].Events[0].MaterialsTotal != 2 {
		t.Errorf("MaterialsTotal=%d", got.Kinds[0].Events[0].MaterialsTotal)
	}
	if len(org.Kinds) != 2 || len(org.Kinds[0].Events) != 2 {
		t.Error("исходное значение изменено")
	}
	staff, ok := visibleOrganization(org, ClosedSet{2: true}, nil, true)
	if !ok || len(staff.Kinds) != 2 || staff.Kinds[0].Events[0].MaterialsTotal != 3 {
		t.Errorf("сотруднику урезано: %+v", staff)
	}
	if _, ok := visibleOrganization(&models.EventOrganizationDetail{}, nil, nil, false); ok {
		t.Error("организация без событий видна")
	}
}
