package api

import "testing"

func TestDocumentSlugBaseTransliterates(t *testing.T) {
	if got := documentSlugBase("Что делать?"); got != "chto-delat" {
		t.Fatalf("слаг из заглавия: %q", got)
	}
}

func TestDocumentSlugBaseFallsBackOnTitleWithoutLetters(t *testing.T) {
	// Заглавие из одних знаков препинания даёт пустую основу, а пустой слаг
	// не адрес: /documents/ — это витрина, и разбор занял бы её собой.
	if got := documentSlugBase("!?…"); got != "razbor" {
		t.Fatalf("запасная основа: %q", got)
	}
}

func TestDocumentSlugBaseStepsAsideFromReservedWords(t *testing.T) {
	// «mine», «review», «new» — литеральные сегменты маршрутов; разбор с
	// таким слагом был бы недостижим (см. router.go, порядок подроутеров).
	// «view», «cuts», «submit» — вторые сегменты коротких адресов: они
	// перехватили бы длинный адрес /documents/{ник}/{слаг}.
	for _, word := range []string{"mine", "review", "new", "edit", "view", "cuts",
		"submit", "unpublish", "approve", "reject"} {
		if got := documentSlugBase(word); got != word+"-razbor" {
			t.Errorf("зарезервированное %q дало %q", word, got)
		}
	}
}
