package book

import (
	"fmt"
	"proofreader/internal/site"
	"strings"
	"time"

	"proofreader/internal/models"
)

// russianMonths — родительный падеж: «14 августа», а не «14 август».
var russianMonths = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// RussianDate печатает дату так, как её пишет читальня.
func RussianDate(t time.Time) string {
	return fmt.Sprintf("%d %s %d г.", t.Day(), russianMonths[int(t.Month())-1], t.Year())
}

// pluralForm выбирает форму существительного по числу n — стандартное
// правило русского счёта: единица (кроме одиннадцати) — one, 2..4
// (кроме 12..14) — few, остальное — many.
func pluralForm(n int, one, few, many string) string {
	mod100 := n % 100
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// CountProofread считает состояние вычитки по статусам страниц.
//
// Пустые страницы в знаменатель не входят: читателю нечего на них вычитывать,
// а в статистике они разбавляли бы долю готового текста.
func CountProofread(statuses []models.PageStatus) Proofread {
	var p Proofread
	for _, s := range statuses {
		switch s {
		case models.PageStatusEmpty:
			continue
		case models.PageStatusProofread:
			p.Human++
		case models.PageStatusMachineProofread:
			p.Machine++
		default:
			p.Raw++
		}
		p.Total++
	}
	return p
}

// SourceLine — выходные данные источника одной строкой: издание, том и
// печатные границы («Сочинения. Том 1, с. 101—103»). Пусто, если нет ни
// издания, ни тома. На титуле она стоит после «Источник: »; отдельной
// функцией — ради шапки текста для нейросети (internal/seo), которой титул
// целиком не нужен.
//
// У подборки издания и единого диапазона нет — строки не будет вовсе. У
// службы или отдельной книги может стоять только одно из двух (в живой базе
// уже есть том с изданием без номера тома) — склеиваем только непустые
// части, чтобы не напечатать огрызок вида ". Том 23, с. 10—20".
func SourceLine(m Meta) string {
	var parts []string
	if m.Edition != "" {
		parts = append(parts, m.Edition)
	}
	if m.Volume != "" {
		parts = append(parts, m.Volume)
	}
	if len(parts) == 0 {
		return ""
	}
	source := joinSourceParts(parts)
	if m.PageFrom > 0 && m.PageTo > 0 {
		source = fmt.Sprintf("%s, с. %d—%d", source, m.PageFrom, m.PageTo)
	}
	return source
}

// TitleLines — строки титульной страницы, общие для всех форматов. Пустая
// строка в срезе означает вертикальный отбой; оформление — дело писателя.
func TitleLines(m Meta) []string {
	var lines []string

	if len(m.Authors) > 0 {
		lines = append(lines, strings.Join(m.Authors, ", "))
	}
	lines = append(lines, m.Title, "")

	// У подборки издания и единого диапазона нет — строки о источнике не
	// будет вовсе. У службы или отдельной книги может стоять только одно из
	// двух (в живой базе уже есть том с изданием без номера тома) — склеиваем
	// только непустые части, а не фиксированную пару через ". ", чтобы не
	// напечатать огрызок вида "Источник: . Том 23, с. 10—20".
	if source := SourceLine(m); source != "" {
		lines = append(lines, "Источник: "+source)
	}
	if m.URL != "" {
		lines = append(lines, site.Name()+": "+m.URL)
	}
	if !m.Modified.IsZero() {
		lines = append(lines, "Состояние текста на "+RussianDate(m.Modified))
	}
	lines = append(lines, "")

	lines = append(lines, proofreadLines(m.Proofread)...)

	if len(m.Missing) > 0 {
		lines = append(lines, "")
		// Капитал намеренный: это заголовок абзаца на титуле, читает
		// человек — предложение с маленькой буквы там неуместно.
		lines = append(lines, "Недоступно на момент выгрузки:")
		for _, name := range m.Missing {
			lines = append(lines, "— "+name)
		}
	}

	return trimEmptyEdges(lines)
}

// joinSourceParts склеивает непустые части "Источника" в одну строку.
//
// 21 из 22 работ живой базы, у которых есть и издание, и номер тома, хранят
// издание, уже оканчивающееся точкой ("...Сочинения, 2-е изд."). Простое
// strings.Join(parts, ". ") на них печатает двойную точку. Разделитель перед
// следующей частью — точка с пробелом, если предыдущая часть ещё не кончается
// на точку, восклицательный, вопросительный знак или многоточие; иначе —
// один пробел. Хвостовой пробел в названии издания не должен обмануть
// проверку, поэтому перед сравнением он обрезается.
func joinSourceParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		sep := ". "
		trimmed := strings.TrimRight(result, " \t")
		for _, mark := range []string{".", "!", "?", "…"} {
			if strings.HasSuffix(trimmed, mark) {
				sep = " "
				break
			}
		}
		result += sep + part
	}
	return result
}

// trimEmptyEdges снимает пустые строки по краям среза. Полностью пустая
// глава (одни пустая_страница) даёт Proofread.Total == 0 — это легальное
// состояние, а не повод оставить в файле висящий хвостовой перевод строки.
func trimEmptyEdges(lines []string) []string {
	start := 0
	for start < len(lines) && lines[start] == "" {
		start++
	}
	end := len(lines)
	for end > start && lines[end-1] == "" {
		end--
	}
	return lines[start:end]
}

func proofreadLines(p Proofread) []string {
	if p.Total == 0 {
		return nil
	}
	if p.Human == p.Total {
		// Без «все»: «все 21 страница» само по себе режет слух, а перед
		// «выверены» ещё и рассогласовано в числе с «страница». Склоняем
		// заодно глагол тем же правилом — «21 страница выверена», «2
		// страницы выверены», «5 страниц выверены» — и «все N» не нужно
		// вовсе, единица больше не требует отдельной ветки.
		noun := pluralForm(p.Total, "страница", "страницы", "страниц")
		verb := pluralForm(p.Total, "выверена", "выверены", "выверены")
		return []string{fmt.Sprintf("Вычитано полностью: %d %s %s человеком.", p.Total, noun, verb)}
	}
	word := pluralForm(p.Total, "страницы", "страниц", "страниц")
	return []string{
		fmt.Sprintf("Из %d %s вычитано человеком — %d, машиной — %d, не вычитано — %d.",
			p.Total, word, p.Human, p.Machine, p.Raw),
		"Текст распознан автоматически; человеческая вычитка не завершена.",
		"Возможны ошибки — сверяйтесь со сканом в читальне.",
	}
}
