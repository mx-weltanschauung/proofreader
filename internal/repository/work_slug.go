package repository

import (
	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

// workSlugFrom собирает слаг работы. Единственное место, где роль работы
// встречается со слагом: pkg/slug о ролях не знает намеренно — он должен
// оставаться проверяемым без моделей и без базы.
//
// Служебные передние листы берут координаты у родителя (их подаёт запрос,
// см. worksWithSlug): свой заголовок у них — «В. И. Ленин. Полное собрание
// сочинений. Том 11. Предваряющие материалы», сорок с лишним знаков,
// повторяющих автора и издание ради одного различающего слова.
func workSlugFrom(role, editionSlug string, number *int, part *string, precedes *int, title string) string {
	if editionSlug != "" {
		switch role {
		case models.WorkRoleVolume:
			if tag := slug.VolumeTag(number, part); tag != "" {
				return editionSlug + "-" + tag
			}
		case models.WorkRoleFrontMatter:
			if tag := slug.VolumeTag(number, part); tag != "" {
				return editionSlug + "-" + tag + "-front"
			}
		case models.WorkRoleEditionFrontMatter:
			if tag := slug.VolumeTag(precedes, nil); tag != "" {
				return editionSlug + "-front-" + tag
			}
		}
	}
	return slug.Text(title)
}
