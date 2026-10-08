package seo

import (
	"strings"
	"testing"
)

func TestPurgeEmptiesBothCaches(t *testing.T) {
	h := NewHandler(nil)
	h.cache.put("/works/1", &cacheEntry{body: "том"})
	h.cache.put("/works/1/chapters/2", &cacheEntry{body: "глава"})
	h.cards.put("/og/work/1.png", &cacheEntry{body: "png"})

	pages, cards := h.Purge()
	if pages != 2 || cards != 1 {
		t.Errorf("Purge унёс %d страниц и %d карточек, ожидалось 2 и 1", pages, cards)
	}
	if _, ok := h.cache.get("/works/1"); ok {
		t.Error("страница осталась в кэше после Purge")
	}
	if _, ok := h.cards.get("/og/work/1.png"); ok {
		t.Error("карточка осталась в кэше после Purge")
	}
}

// Счётчик веса не сверяется ни с чем внешним (см. entryWeight), поэтому
// обнулить его при сбросе обязательно: иначе кэш после снятия тома считал бы
// себя полным и вытеснял бы живые записи, ничего при этом не освободив.
func TestPurgeResetsSizeCounter(t *testing.T) {
	h := NewHandler(nil)
	h.cache.put("/works/1", &cacheEntry{body: strings.Repeat("x", 4096)})
	if h.cache.size == 0 {
		t.Fatal("вес не посчитан — тест ниже ничего не проверит")
	}

	h.Purge()

	if h.cache.size != 0 {
		t.Errorf("после Purge вес кэша %d, ожидался 0", h.cache.size)
	}
	if got := h.cache.order.Len(); got != 0 {
		t.Errorf("после Purge в очереди %d записей, ожидалось 0", got)
	}
	if got := len(h.cache.items); got != 0 {
		t.Errorf("после Purge в карте %d записей, ожидалось 0", got)
	}
}

func TestPurgeOnEmptyHandlerIsSilent(t *testing.T) {
	var h *Handler
	if pages, cards := h.Purge(); pages != 0 || cards != 0 {
		t.Errorf("Purge на nil-обработчике: %d, %d", pages, cards)
	}
}

// Сброс обязан переживать рендер, начатый до него. Иначе он отменяется сам
// собой: страница снятого тома, собранная запоздавшим рендером, ложится в кэш
// уже после сброса и живёт там свой час (карточка — сутки), а проверка 410
// внутри команды снятия делается один раз и такого не увидит.
func TestPurgeBeatsARenderThatStartedBeforeIt(t *testing.T) {
	h := NewHandler(nil)

	// Рендер «начался»: метка снята.
	gen := h.cache.generation()

	// Пока он шёл, том сняли.
	h.Purge()

	// Рендер вернулся и пытается положить готовую страницу.
	if h.cache.putIfFresh("/works/45", &cacheEntry{body: "снятый том"}, gen) {
		t.Error("putIfFresh положил страницу, собранную до сброса")
	}
	if _, ok := h.cache.get("/works/45"); ok {
		t.Error("страница снятого тома легла в кэш после сброса")
	}
}

func TestPutIfFreshStoresWhenNothingWasPurged(t *testing.T) {
	h := NewHandler(nil)
	gen := h.cache.generation()
	if !h.cache.putIfFresh("/works/45", &cacheEntry{body: "живой том"}, gen) {
		t.Fatal("putIfFresh отказался писать без единого сброса")
	}
	if _, ok := h.cache.get("/works/45"); !ok {
		t.Error("страница не попала в кэш")
	}
}

// Карточки живут своим кэшем и своим поколением: сброс страниц не должен
// запрещать запись карточек и наоборот.
func TestCardAndPageGenerationsAreSeparate(t *testing.T) {
	h := NewHandler(nil)
	cardGen := h.cards.generation()
	h.Purge()
	if h.cards.putIfFresh("/og/work/45.png", &cacheEntry{body: "png"}, cardGen) {
		t.Error("сброс не сдвинул поколение карточек")
	}
	fresh := h.cards.generation()
	if !h.cards.putIfFresh("/og/work/46.png", &cacheEntry{body: "png"}, fresh) {
		t.Error("после сброса карточки перестали писаться вовсе")
	}
}
