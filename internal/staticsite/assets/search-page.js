// Страница поиска статической читальни: заглавия — из window.TITLES (грузится
// тегом script и потому работает через file://), текст — pagefind, только
// когда папку отдаёт сервер: через file:// браузер не даёт ему грузить индекс.
(function () {
  'use strict';
  var S = window.ChitalnyaSearch;
  var root = '../';
  var KIND = { work: 'Том', chapter: 'Глава', concept: 'Понятие', collection: 'Подборка', document: 'Разбор' };
  var q = (new URLSearchParams(location.search).get('q') || '').trim();
  var titlesOut = document.getElementById('titles');
  var fullOut = document.getElementById('fulltext');
  var editionSelect = document.getElementById('edition');
  document.getElementById('q').value = q;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  function hint(out, text) {
    out.textContent = '';
    out.appendChild(el('p', 'hint', text));
  }

  function showTitles() {
    titlesOut.textContent = '';
    if (!q) return;
    if (S.tooShort(q)) {
      hint(titlesOut, 'Запрос короче двух знаков.');
      return;
    }
    var r = S.match(window.TITLES || [], q, 200);
    titlesOut.appendChild(el('h2', null, 'В заглавиях — ' + r.total));
    var ul = el('ul', 'results');
    r.items.forEach(function (t) {
      var li = el('li');
      li.appendChild(el('span', 'kind', KIND[t[0]] || ''));
      var a = el('a', null, t[1]);
      a.href = root + t[3];
      li.appendChild(a);
      if (t[2]) li.appendChild(el('span', 'sub', t[2]));
      ul.appendChild(li);
    });
    titlesOut.appendChild(ul);
  }

  var pagefind = null;

  function runFullText() {
    var filters = {};
    if (editionSelect.value) filters['издание'] = editionSelect.value;
    hint(fullOut, 'Ищу в тексте…');
    pagefind.search(q, { filters: filters }).then(function (search) {
      var shown = search.results.slice(0, 30);
      return Promise.all(shown.map(function (r) { return r.data(); })).then(function (data) {
        fullOut.textContent = '';
        fullOut.appendChild(el('h2', null, 'В тексте — ' + search.results.length +
          (search.results.length > shown.length ? ' (показаны первые ' + shown.length + ')' : '')));
        var ul = el('ul', 'results');
        data.forEach(function (d) {
          var r = S.resultTarget(d);
          var li = el('li');
          var a = el('a', null, r.title);
          a.href = r.url;
          li.appendChild(a);
          if (r.volume) li.appendChild(el('span', 'sub', r.volume));
          var p = el('p', 'excerpt');
          // Отрывок pagefind уже экранирован и несёт только <mark>.
          p.innerHTML = r.excerpt;
          li.appendChild(p);
          ul.appendChild(li);
        });
        fullOut.appendChild(ul);
      });
    }).catch(function () {
      hint(fullOut, 'Поиск по тексту не сработал.');
    });
  }

  function startFullText() {
    if (!q || S.tooShort(q)) return;
    if (location.protocol === 'file:') {
      hint(fullOut, 'Поиск по тексту работает, когда папку открывает сервер: запустите serve из папки читальни ' +
        '(или «python3 -m http.server») и откройте адрес, который он напечатает. Поиск по заглавиям работает и так.');
      return;
    }
    hint(fullOut, 'Загружаю индекс…');
    import(new URL(root + 'pagefind/pagefind.js', location.href).href).then(function (pf) {
      pagefind = pf;
      return Promise.resolve(pf.options({ baseUrl: new URL(root, location.href).pathname }));
    }).then(function () {
      return pagefind.filters();
    }).then(function (filters) {
      var editions = Object.keys((filters && filters['издание']) || {}).sort();
      editions.forEach(function (name) {
        var o = el('option', null, name);
        o.value = name;
        editionSelect.appendChild(o);
      });
      editionSelect.hidden = editions.length < 2;
      editionSelect.onchange = runFullText;
      runFullText();
    }).catch(function () {
      hint(fullOut, 'В этой копии нет индекса поиска по тексту.');
    });
  }

  showTitles();
  startFullText();
})();
