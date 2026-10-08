package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"proofreader/internal/models"
)

// fakeFragmentStore — подставное хранилище вырезок с записью вызовов.
type fakeFragmentStore struct {
	byPage    map[int64][]*models.IndexFragment
	byRefs    map[int64][]*models.IndexFragment
	anchors   map[int64][4]any
	statuses  map[int64]string
	replaced  map[int64][]*models.IndexFragment
	failOnGet bool

	// reanchoredPages отмечает страницы, для которых звали ByPage — то есть
	// переякоривание реально запускалось. Карта нулевого fakeFragmentStore{}
	// не инициализирована заранее: ByPage заводит её лениво при первом вызове.
	reanchoredPages map[int64]bool
}

func newFakeFragmentStore() *fakeFragmentStore {
	return &fakeFragmentStore{
		byPage:   map[int64][]*models.IndexFragment{},
		byRefs:   map[int64][]*models.IndexFragment{},
		anchors:  map[int64][4]any{},
		statuses: map[int64]string{},
		replaced: map[int64][]*models.IndexFragment{},
	}
}

func (f *fakeFragmentStore) ByReferences(ctx context.Context, ids []int64) (map[int64][]*models.IndexFragment, error) {
	out := map[int64][]*models.IndexFragment{}
	for _, id := range ids {
		if list, ok := f.byRefs[id]; ok {
			out[id] = list
		}
	}
	return out, nil
}

func (f *fakeFragmentStore) ByPage(ctx context.Context, pageID int64) ([]*models.IndexFragment, error) {
	if f.reanchoredPages == nil {
		f.reanchoredPages = map[int64]bool{}
	}
	f.reanchoredPages[pageID] = true
	if f.failOnGet {
		return nil, context.DeadlineExceeded
	}
	return f.byPage[pageID], nil
}

func (f *fakeFragmentStore) ReplaceForReference(ctx context.Context, refID int64, list []*models.IndexFragment) error {
	f.replaced[refID] = list
	return nil
}

func (f *fakeFragmentStore) UpdateAnchor(ctx context.Context, id int64, start, end int, startHash, endHash string) error {
	f.anchors[id] = [4]any{start, end, startHash, endHash}
	return nil
}

func (f *fakeFragmentStore) SetStatus(ctx context.Context, id int64, status string) error {
	f.statuses[id] = status
	return nil
}

func TestFindQuotePicksNearestOccurrence(t *testing.T) {
	text := "цитата в начале … цитата в конце"
	// Одна и та же цитата встречается дважды; выбор — ближайший к прежнему
	// месту, иначе вырезка перепрыгнет на чужое вхождение.
	first := strings.Index(text, "цитата")
	second := strings.LastIndex(text, "цитата")

	if got, ok := findQuote(text, "цитата", first+2); !ok || got != first {
		t.Fatalf("около начала: %d, %v; ожидалось %d", got, ok, first)
	}
	if got, ok := findQuote(text, "цитата", second+5); !ok || got != second {
		t.Fatalf("около конца: %d, %v; ожидалось %d", got, ok, second)
	}
	if _, ok := findQuote(text, "нет такого", 0); ok {
		t.Fatal("несуществующая цитата найдена")
	}
	if _, ok := findQuote(text, "", 0); ok {
		t.Fatal("пустая цитата найдена")
	}
}

// anchorQuoteLen делится на 1, 2, 3 и 4 — на любую ширину руны в UTF-8.
// Поэтому текст из рун одинаковой ширины (например, только "я") на 120-м
// байте всегда попадает на границу руны сам по себе, и такой текст не
// проверяет подгонку в quoteOf вообще: cut[:120] дал бы тот же результат,
// что и защищённая версия. Неудобный случай получается только смесью
// ширин, подобранной так, чтобы 120-й байт заведомо приходился на середину
// многобайтовой руны.

func TestQuoteOfCutsOnRuneBoundaryHead(t *testing.T) {
	// 119 однобайтовых символов, потом кириллица: руна начинается на байте
	// 119 и занимает байты 119-120, значит cut[:120] разрубил бы её пополам.
	// Без подгонки по utf8.RuneStart цитата вышла бы длиной ровно 120 байт
	// и половиной битой руны на конце.
	text := strings.Repeat("a", 119) + strings.Repeat("я", 50)
	head := quoteOf(text, 0, len(text), true)

	if len(head) > anchorQuoteLen {
		t.Fatalf("цитата длиной %d байт, потолок %d", len(head), anchorQuoteLen)
	}
	if len(head) != 119 {
		t.Fatalf("голова длиной %d байт, ожидалось 119 (защита должна была отступить с 120)", len(head))
	}
	if !utf8.ValidString(head) {
		t.Fatalf("голова не валидна как UTF-8: %q", head)
	}
	// Обрезка посреди руны сделала бы цитату небайтовым мусором, который
	// потом никогда не найдётся в тексте.
	if !strings.HasPrefix(text, head) {
		t.Fatalf("голова не является префиксом текста: %q", head)
	}
}

func TestQuoteOfCutsOnRuneBoundaryTail(t *testing.T) {
	// Хвост отсчитывается от конца (len(cut)-120), поэтому неудобный случай
	// получается, когда однобайтовые символы стоят после многобайтовых:
	// 100 двухбайтовых "я" (байты 0..199) и один байт "a" (байт 200), длина
	// 201. Отсчёт от конца даёт смещение 81 — нечётное, то есть середину
	// одной из рун "я". Без подгонки цитата вышла бы длиной ровно 120 байт
	// и начиналась бы с половины битой руны.
	text := strings.Repeat("я", 100) + "a"
	tail := quoteOf(text, 0, len(text), false)

	if len(tail) > anchorQuoteLen {
		t.Fatalf("цитата длиной %d байт, потолок %d", len(tail), anchorQuoteLen)
	}
	if len(tail) != 119 {
		t.Fatalf("хвост длиной %d байт, ожидалось 119 (защита должна была отступить с 120)", len(tail))
	}
	if !utf8.ValidString(tail) {
		t.Fatalf("хвост не валиден как UTF-8: %q", tail)
	}
	if !strings.HasSuffix(text, tail) {
		t.Fatalf("хвост не является суффиксом текста: %q", tail)
	}
}

func TestQuoteOfShortCutTakesWhole(t *testing.T) {
	text := "короткая вырезка целиком"
	if got := quoteOf(text, 0, len(text), true); got != text {
		t.Fatalf("короткая вырезка дала цитату %q", got)
	}
}

func TestQuoteOfEmptyCutIsEmpty(t *testing.T) {
	// Пустая вырезка (start == end): sliceCut отдаёт "", quoteOf не должен
	// на этом падать или что-то придумывать.
	if got := quoteOf("какой-то текст", 5, 5, true); got != "" {
		t.Fatalf("пустая вырезка дала цитату %q", got)
	}
}

func TestQuoteOfCutLongerThanTextIsClamped(t *testing.T) {
	// Цитата длиннее всей страницы: end уходит далеко за len(text), sliceCut
	// обязан обрезать по границе текста, а не паниковать по индексу.
	text := "вся страница целиком"
	if got := quoteOf(text, 0, len(text)+1000, false); got != text {
		t.Fatalf("вырезка длиннее текста дала %q, ожидался весь текст", got)
	}
}

func TestReanchorFragmentFollowsShiftedText(t *testing.T) {
	f := &models.IndexFragment{
		StartPageID: 1, EndPageID: 1, StartOffset: 0, EndOffset: 12,
		HeadQuote: "первый абзац", TailQuote: "первый абзац",
		Status: models.FragmentStatusConfirmed,
	}
	// Выше вырезки дописали строку: смещения уехали, текст цел.
	newText := "новая строка сверху\n\nпервый абзац\n\nвторой абзац"

	start, end, ok := reanchor(f.Anchor(), 1, newText)

	if !ok {
		t.Fatal("переякоривание не удалось на целом тексте")
	}
	if newText[start:end] != "первый абзац" {
		t.Fatalf("вырезка стала %q", newText[start:end])
	}
}

func TestReanchorFragmentFailsWhenQuoteEdited(t *testing.T) {
	f := &models.IndexFragment{
		StartPageID: 1, EndPageID: 1, StartOffset: 0, EndOffset: 12,
		HeadQuote: "первый абзац", TailQuote: "первый абзац",
	}
	// Правка внутри самой цитаты — нечёткого поиска нет намеренно.
	if _, _, ok := reanchor(f.Anchor(), 1, "первьй абзац\n\nвторой"); ok {
		t.Fatal("правка внутри цитаты прошла как успех")
	}
}

func TestReanchorFragmentAcrossPagesTouchesOneSide(t *testing.T) {
	f := &models.IndexFragment{
		StartPageID: 1, StartOffset: 5, EndPageID: 2, EndOffset: 9,
		HeadQuote: "голова", TailQuote: "хвост",
	}
	// Правится только вторая страница: начало обязано остаться прежним.
	start, end, ok := reanchor(f.Anchor(), 2, "ааа хвост ххх")
	if !ok {
		t.Fatal("переякоривание конца не удалось")
	}
	if start != 5 {
		t.Fatalf("начало сдвинулось на %d", start)
	}
	if end != len("ааа хвост") {
		t.Fatalf("конец %d, ожидался %d", end, len("ааа хвост"))
	}
}

// TestReanchorFragmentTailNearIsQuoteStartNotQuoteEnd — находка финального
// разбора: near для хвостового поиска считался как f.EndOffset-from, то есть
// концом цитаты, а findQuote возвращает НАЧАЛО вхождения. При двух вхождениях
// одной фразы рядом это на длину цитаты сбивало выбор в сторону дальнего —
// здесь второе вхождение стоит к ошибочному near ближе, чем настоящее первое.
func TestReanchorFragmentTailNearIsQuoteStartNotQuoteEnd(t *testing.T) {
	quote := "конец"
	prefix := "ааа "
	text := prefix + quote + "!!!!!" + quote
	first := len(prefix)
	quoteLen := len(quote)

	f := &models.IndexFragment{
		StartPageID: 1, EndPageID: 2,
		// EndOffset — старое смещение конца вырезки (сразу после первого
		// вхождения в прежнем тексте страницы); только конец на этой странице.
		EndOffset: first + quoteLen,
		TailQuote: quote,
	}

	_, end, ok := reanchor(f.Anchor(), 2, text)
	if !ok {
		t.Fatal("переякоривание хвоста не удалось")
	}
	// Верный ближайший конец — сразу после ПЕРВОГО вхождения. С багом
	// (near = EndOffset без вычета длины цитаты) выбиралось второе вхождение,
	// потому что оно на 5 байт ближе к неверному near, чем первое — на 10.
	if want := first + quoteLen; end != want {
		t.Fatalf("конец вырезки %d, ожидался %d (первое вхождение, не второе)", end, want)
	}
}

func TestReanchorPageUpdatesAndMarksStale(t *testing.T) {
	store := newFakeFragmentStore()
	good := &models.IndexFragment{
		ID: 10, StartPageID: 7, EndPageID: 7, StartOffset: 0, EndOffset: 6,
		HeadQuote: "первый", TailQuote: "первый", Status: models.FragmentStatusConfirmed,
	}
	bad := &models.IndexFragment{
		ID: 11, StartPageID: 7, EndPageID: 7, StartOffset: 0, EndOffset: 6,
		HeadQuote: "исчезнувшая цитата", TailQuote: "исчезнувшая цитата",
		Status: models.FragmentStatusMachine,
	}
	store.byPage[7] = []*models.IndexFragment{good, bad}

	if err := reanchorPage(context.Background(), store, 7, "сверху\n\nпервый"); err != nil {
		t.Fatalf("reanchorPage: %v", err)
	}

	if _, ok := store.anchors[10]; !ok {
		t.Fatal("целая вырезка не переякорена")
	}
	if store.statuses[10] != "" {
		t.Fatalf("статус целой вырезки тронут: %q", store.statuses[10])
	}
	if store.statuses[11] != models.FragmentStatusStale {
		t.Fatalf("отвязавшаяся вырезка получила статус %q", store.statuses[11])
	}
	// Смещения отвязавшейся не затираются: человеку надо видеть, где было.
	if _, ok := store.anchors[11]; ok {
		t.Fatal("смещения отвязавшейся вырезки переписаны")
	}
}

// TestReanchorPageContinuesAfterOneFragmentStoreError — находка финального
// разбора: reanchorPage возвращался при первой же ошибке записи, оставляя
// остальные вырезки страницы непереякоренными без какого-либо следа этого в
// ответе. Тут вторая вырезка (ID 11) обязана быть переякорена, даже если
// запись первой (ID 10) отказала.
type failingAnchorStore struct {
	*fakeFragmentStore
	failID int64
}

func (s *failingAnchorStore) UpdateAnchor(ctx context.Context, id int64, start, end int, startHash, endHash string) error {
	if id == s.failID {
		return errors.New("запись якоря отказала")
	}
	return s.fakeFragmentStore.UpdateAnchor(ctx, id, start, end, startHash, endHash)
}

func TestReanchorPageContinuesAfterOneFragmentStoreError(t *testing.T) {
	inner := newFakeFragmentStore()
	store := &failingAnchorStore{fakeFragmentStore: inner, failID: 10}
	first := &models.IndexFragment{
		ID: 10, StartPageID: 7, EndPageID: 7, StartOffset: 0, EndOffset: 6,
		HeadQuote: "первый", TailQuote: "первый", Status: models.FragmentStatusConfirmed,
	}
	second := &models.IndexFragment{
		ID: 11, StartPageID: 7, EndPageID: 7, StartOffset: 20, EndOffset: 27,
		HeadQuote: "второй", TailQuote: "второй", Status: models.FragmentStatusConfirmed,
	}
	inner.byPage[7] = []*models.IndexFragment{first, second}

	err := reanchorPage(context.Background(), store, 7, "сверху\n\nпервый\n\nещё\n\nвторой")

	if err == nil {
		t.Fatal("ошибка записи первой вырезки проглочена")
	}
	// Вторая вырезка обязана быть переякорена несмотря на отказ первой —
	// иначе одна неудачная запись глушит переякоривание всей страницы.
	if _, ok := inner.anchors[11]; !ok {
		t.Fatal("вторая вырезка не переякорена после ошибки на первой")
	}
}

func TestReanchorPageSkipsWhenHashMatches(t *testing.T) {
	store := newFakeFragmentStore()
	text := "текст страницы"
	store.byPage[7] = []*models.IndexFragment{{
		ID: 10, StartPageID: 7, EndPageID: 7, StartOffset: 0, EndOffset: 5,
		HeadQuote: "текст", TailQuote: "текст",
		StartHash: pageHash(text), EndHash: pageHash(text),
	}}

	if err := reanchorPage(context.Background(), store, 7, text); err != nil {
		t.Fatalf("reanchorPage: %v", err)
	}

	// Хеш совпал — работы нет.
	if len(store.anchors) != 0 || len(store.statuses) != 0 {
		t.Fatalf("совпавший хеш вызвал запись: %v %v", store.anchors, store.statuses)
	}
}

func TestReanchorPageRevivesStaleFragment(t *testing.T) {
	store := newFakeFragmentStore()
	store.byPage[7] = []*models.IndexFragment{{
		ID: 10, StartPageID: 7, EndPageID: 7, StartOffset: 0, EndOffset: 6,
		HeadQuote: "первый", TailQuote: "первый", Status: models.FragmentStatusStale,
	}}

	// Страницу вернули к прежнему тексту — вырезка снова находится и
	// перестаёт быть отвязавшейся.
	if err := reanchorPage(context.Background(), store, 7, "первый абзац"); err != nil {
		t.Fatalf("reanchorPage: %v", err)
	}
	if store.statuses[10] != models.FragmentStatusMachine {
		t.Fatalf("ожил со статусом %q, ожидался machine", store.statuses[10])
	}
}
