import { afterEach, describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { existsSync, readFileSync } from 'node:fs';
import { Legal, LegalView } from './Legal';
import { resetSiteForTests } from '../services/site';

/**
 * Текст правовой информации — свойство экземпляра (instance/legal.html), а не
 * платформы. Эти проверки сторожат текст НАШЕГО экземпляра и идут, только
 * если файл рядом; в публичной платформе его нет, и набор пропускается.
 */
const LEGAL_FILE = '../instance/legal.html';
const legalHtml = existsSync(LEGAL_FILE) ? readFileSync(LEGAL_FILE, 'utf8') : '';

function renderLegal(path = '/legal') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LegalView html={legalHtml} />
    </MemoryRouter>,
  );
}

// Платформа не раздаёт чужим операторам готовых юридических утверждений о
// них самих: без файла экземпляра страницы нет — как любого неизвестного
// адреса, уход на главную.
describe('страница без файла экземпляра', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    resetSiteForTests();
  });

  it('уводит на главную', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 404 }));
    render(
      <MemoryRouter initialEntries={['/legal']}>
        <Routes>
          <Route path="/legal" element={<Legal />} />
          <Route path="/" element={<p>главная</p>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText('главная')).toBeInTheDocument();
  });
});

describe.skipIf(!legalHtml)('правовая информация экземпляра', () => {
  it('разбита на три раздела', () => {
    renderLegal();
    expect(screen.getByRole('heading', { name: 'Возрастная маркировка' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Персональные данные' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Издания и авторские права' })).toBeInTheDocument();
  });

  // Знак «18+» в подвале ведёт на /legal#age, форма обращения — на
  // /legal#personal-data. Якоря обязаны существовать: ссылка в никуда здесь
  // не падает, а молча открывает страницу сверху.
  it('держит якоря, на которые ссылаются подвал и форма', () => {
    const { container } = renderLegal();
    for (const id of ['age', 'personal-data', 'reader-account', 'copyright', 'takedown']) {
      expect(container.querySelector(`#${id}`), `нет якоря #${id}`).not.toBeNull();
    }
  });

  // Адрес обязан быть рабочим: без него правообладателю некуда обращаться.
  it('даёт рабочий адрес для обращений', () => {
    const { container } = renderLegal();
    const mailto = [...container.querySelectorAll('a')].filter((a) =>
      (a.getAttribute('href') ?? '').startsWith('mailto:'),
    );
    expect(mailto.length).toBeGreaterThan(0);
    const first = mailto[0].getAttribute('href') ?? '';
    expect(first).toMatch(/^mailto:[^\s@]+@[^\s@]+\.[^\s@]+$/);
    for (const link of mailto) {
      expect(link).toHaveAttribute('href', first);
    }
  });

  // Главное утверждение страницы. Стоит кому-нибудь вернуть в форму поле
  // контакта — раздел начнёт врать, и врать в юридически значимом тексте.
  it('утверждает, что персональные данные не собираются', () => {
    renderLegal();
    expect(screen.getByRole('heading', { name: 'Персональные данные' })).toBeInTheDocument();
    expect(screen.getByText(/Читальня их не собирает/)).toBeInTheDocument();
  });

  // Учётная запись читателя — единственное из этого раздела, что реально
  // уезжает на сервер и там остаётся. Раздел писался под прежний
  // предъявительский билет и по одной фразе не правится: любое из этих
  // утверждений может выпасть по отдельности и остаться незамеченным, пока
  // страница «мы ничего не собираем» продолжает так утверждать.
  describe('учётная запись читателя — что на самом деле хранится', () => {
    function accountSectionText() {
      const { container } = renderLegal();
      // Якорь стоит на заголовке, как у #ip и #takedown, поэтому спрашиваем
      // раздел целиком: важно не где лежит текст, а что утверждение в
      // разделе есть.
      const section = container.querySelector('#personal-data');
      expect(section, 'нет раздела #personal-data').not.toBeNull();
      return section?.textContent ?? '';
    }

    it('называет ник и объясняет, что он публичен', () => {
      const text = accountSectionText();
      expect(text, 'раздел не упоминает ник').toMatch(/ник/i);
      expect(text, 'раздел не объясняет, что ник виден редактору').toMatch(
        /видел? редактору|публичен/i,
      );
    });

    it('хранит хэш пароля, а не сам пароль', () => {
      const text = accountSectionText();
      expect(text, 'раздел не говорит про хэш пароля').toMatch(/хэш пароля/i);
      expect(text, 'раздел не отрицает хранение самого пароля').toMatch(/не сам пароль/i);
    });

    it('признаёт хранение отметки адреса при входе и заведении учётки', () => {
      const text = accountSectionText();
      expect(text, 'раздел не упоминает отметку адреса').toMatch(/отметка адреса/i);
      expect(text, 'раздел не называет срок хранения отметки').toMatch(/суток/i);
    });

    it('заявляет, что почты не спрашивает и не хранит', () => {
      const text = accountSectionText();
      expect(text, 'раздел не отрицает сбор почты').toMatch(
        /почт[ыу].{0,20}не (спрашивает|хранит)/i,
      );
    });

    it('прямо говорит: восстановления пароля нет', () => {
      const text = accountSectionText();
      expect(text, 'раздел не говорит, что пароль не восстановить').toMatch(
        /восстановить нечем|восстановления.*нет/i,
      );
      expect(text, 'раздел не называет цену забытого пароля').toMatch(/безвозвратн/i);
    });

    it('объявляет прежний билет недействительным', () => {
      const text = accountSectionText();
      expect(text, 'раздел не упоминает прежний билет').toMatch(/билет/i);
      expect(text, 'раздел не говорит, что билет упразднён').toMatch(/упраздн/i);
    });

    it('не оставляет мёртвую ссылку на прежний перенос билета', () => {
      const { container } = renderLegal();
      const suggestionsMineLink = [...container.querySelectorAll('a')].find(
        (a) => a.getAttribute('href') === '/suggestions/mine',
      );
      expect(
        suggestionsMineLink,
        'страница всё ещё ссылается на устаревший перенос билета',
      ).toBeUndefined();
    });
  });

  // Порядок снятия материала — единственное, что реально снижает риск
  // «неоднократности» при обращении в суд. Он обязан быть на странице.
  it('объясняет правообладателю, что делать', () => {
    renderLegal();
    expect(screen.getByRole('heading', { name: 'Если вы правообладатель' })).toBeInTheDocument();
    // На этом держится позиция «ПДн не обрабатываем» при живом почтовом
    // ящике: письма читает человек, в базу не складываются — обработка без
    // средств автоматизации, п. 8 ст. 22 ч. 2. Фраза один раз уже потерялась
    // при переформатировании и уехала из отчёта как сделанная.
    expect(screen.getByText(/в базу читальни они не попадают/)).toBeInTheDocument();
  });

  it('не оставляет ссылок в никуда', () => {
    const { container } = renderLegal();
    const dead = [...container.querySelectorAll('a')].filter(
      (a) => (a.getAttribute('href') ?? '') === '#',
    );
    expect(dead).toEqual([]);
  });
});
