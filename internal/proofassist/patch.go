package proofassist

import (
	"fmt"
	"slices"
	"sort"
	"unicode"
)

// Edit заменяет полуоткрытый диапазон кодовых точек исходного Markdown.
type Edit struct {
	StartRune, EndRune int
	Old, New           string
}

// Apply применяет правки к исходным позициям и сохраняет байты вне их диапазонов.
func Apply(base string, edits []Edit) (string, error) {
	offsets := make([]int, 0)
	for offset := range base {
		offsets = append(offsets, offset)
	}
	offsets = append(offsets, len(base))

	ordered := slices.Clone(edits)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].StartRune == ordered[j].StartRune {
			return ordered[i].EndRune > ordered[j].EndRune
		}
		return ordered[i].StartRune > ordered[j].StartRune
	})

	size := len(base)
	for i, edit := range ordered {
		if edit.StartRune < 0 || edit.EndRune < edit.StartRune || edit.EndRune >= len(offsets) {
			return "", fmt.Errorf("недопустимый диапазон [%d, %d)", edit.StartRune, edit.EndRune)
		}
		if i > 0 {
			next := ordered[i-1]
			if edit.EndRune > next.StartRune ||
				(edit.StartRune == next.StartRune && edit.EndRune == next.EndRune) {
				return "", fmt.Errorf("пересечение правок у позиции %d", edit.EndRune)
			}
		}
		start, end := offsets[edit.StartRune], offsets[edit.EndRune]
		if base[start:end] != edit.Old {
			return "", fmt.Errorf("исходный фрагмент не совпадает в диапазоне [%d, %d)", edit.StartRune, edit.EndRune)
		}
		size += len(edit.New) - len(edit.Old)
	}

	result := make([]byte, size)
	write, tail := len(result), len(base)
	for _, edit := range ordered {
		start, end := offsets[edit.StartRune], offsets[edit.EndRune]
		write -= tail - end
		copy(result[write:], base[end:tail])
		write -= len(edit.New)
		copy(result[write:], edit.New)
		tail = start
	}
	copy(result[:write], base[:tail])

	markdown := string(result)
	for _, r := range markdown {
		// Те же исключения, что и при подаче предложения к полосе.
		if r == '\n' || r == '\r' || r == '\t' || r == '\u00ad' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", fmt.Errorf("невидимый символ %U в итоговом Markdown", r)
		}
	}
	return markdown, nil
}
