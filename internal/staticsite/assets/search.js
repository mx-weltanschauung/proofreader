// Поиск по заглавиям статической читальни. Обычный скрипт, не модуль:
// модули через file:// браузер не грузит. В Node (тест) отдаётся через
// module.exports, в браузере — window.ChitalnyaSearch.
(function (root) {
  'use strict';
  // Тот же порог, что minQueryRunes в internal/models/search.go.
  var MIN = 2;
  var ORDER = { work: 0, concept: 1, chapter: 2, collection: 3, document: 4 };

  function normalize(s) {
    return String(s).toLowerCase().replace(/ё/g, 'е');
  }

  function words(s) {
    return normalize(s).split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  }

  function tooShort(q) {
    return Array.from(normalize(q).trim()).length < MIN;
  }

  function rank(kind) {
    return kind in ORDER ? ORDER[kind] : 9;
  }

  // match — записи [вид, заглавие, подпись, путь], у которых каждое слово
  // запроса — начало какого-нибудь слова заглавия. Стеммера нет: начало
  // слова ловит падежи так же, как подсветка читальни.
  function match(entries, query, limit) {
    var q = words(query);
    if (tooShort(query) || q.length === 0) return { total: 0, items: [] };
    var hits = [];
    for (var i = 0; i < entries.length; i++) {
      var e = entries[i];
      var w = e._w || (e._w = words(e[1]));
      var ok = q.every(function (qw) {
        return w.some(function (tw) { return tw.lastIndexOf(qw, 0) === 0; });
      });
      if (ok) hits.push(i);
    }
    hits.sort(function (a, b) {
      return rank(entries[a][0]) - rank(entries[b][0]) || a - b;
    });
    return {
      total: hits.length,
      items: hits.slice(0, limit).map(function (i) { return entries[i]; }),
    };
  }

  // resultTarget — куда ведёт результат pagefind и как он подписан. Файл
  // главы бывает в сотни полос, поэтому ссылка идёт на тот подрезультат
  // (часть под заголовком подглавы с якорем ch-<id>), где совпадений больше
  // всего, а подпись несёт том: одинаковых заглавий глав в корпусе много.
  function resultTarget(d) {
    var best = null;
    (d.sub_results || []).forEach(function (s) {
      var n = (s.locations || []).length;
      if (!best || n > (best.locations || []).length) best = s;
    });
    var meta = d.meta || {};
    var title = meta.title || d.url;
    // Заголовок текста часто повторяет заглавие главы (с номером сноски
    // хвостом) — такой подзаголовок подпись не удлиняет.
    if (best && best.title && normalize(best.title).indexOf(normalize(title)) !== 0) {
      title += ' — ' + best.title;
    }
    return {
      url: best ? best.url : d.url,
      title: title,
      volume: meta.volume || '',
      excerpt: best && best.excerpt ? best.excerpt : d.excerpt,
    };
  }

  var api = {
    MIN: MIN, normalize: normalize, words: words, tooShort: tooShort, match: match,
    resultTarget: resultTarget,
  };
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  } else {
    root.ChitalnyaSearch = api;
  }
})(this);
