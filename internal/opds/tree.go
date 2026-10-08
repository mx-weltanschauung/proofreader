package opds

// Prune — дерево глав, как его видит каталог: без аппарата (поддерево
// целиком — признак стоит на корне поддерева) и без узлов, в чьём диапазоне
// нет ни одной полосы (их выгрузка отвечает 404). Входное дерево не
// трогается: узлы копируются.
func Prune(nodes []*Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if n.IsApparatus || !n.HasPages {
			continue
		}
		c := *n
		c.Children = Prune(n.Children)
		out = append(out, &c)
	}
	return out
}

// find ищет узел в отсеянном дереве. nil — нет его там: такой главы нет,
// либо она внутри аппарата или без полос.
func find(nodes []*Node, id int64) *Node {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
		if f := find(n.Children, id); f != nil {
			return f
		}
	}
	return nil
}

// window — границы страницы page (с 1) списка из n записей по size. На первой
// странице ok всегда, даже у пустого списка; дальше — только если на
// странице есть хоть одна запись.
func window(n, size, page int) (from, to int, ok bool) {
	if page < 1 {
		return 0, 0, false
	}
	from = (page - 1) * size
	if page > 1 && from >= n {
		return 0, 0, false
	}
	to = min(from+size, n)
	return from, to, true
}
