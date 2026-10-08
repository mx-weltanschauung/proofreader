package repository

import (
	"testing"

	"proofreader/internal/models"
)

func TestWorkSlugFrom(t *testing.T) {
	six, eleven, twentySix := 6, 11, 26
	part2 := "II"
	cases := []struct {
		name                     string
		role, editionSlug, title string
		number, precedes         *int
		part                     *string
		want                     string
	}{
		{"том", models.WorkRoleVolume, "lenin", "В. И. Ленин. ПСС. Том 6", &six, nil, nil, "lenin-t06"},
		{"полутом", models.WorkRoleVolume, "mae", "Сочинения. Том 26. Часть II", &twentySix, nil, &part2, "mae-t26-ii"},
		{"передние листы", models.WorkRoleFrontMatter, "lenin", "Том 11. Предваряющие материалы", &eleven, nil, nil, "lenin-t11-front"},
		{"предисловие к изданию", models.WorkRoleEditionFrontMatter, "lenin", "От редакции", nil, &eleven, nil, "lenin-front-t11"},
		// Ни собрания, ни номера: слаг собирается из заголовка.
		{"работа сама по себе", models.WorkRoleVolume, "", "Диалектика природы", nil, nil, nil, "dialektika-prirody"},
	}
	for _, c := range cases {
		got := workSlugFrom(c.role, c.editionSlug, c.number, c.part, c.precedes, c.title)
		if got != c.want {
			t.Errorf("%s: workSlugFrom(...) = %q, ожидалось %q", c.name, got, c.want)
		}
	}
}
