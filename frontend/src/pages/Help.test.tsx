import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { Help } from './Help';
import { staticArchiveApi } from '../services/api';
import { HINT_IDS } from '../hints/registry';
import { HINTS_KEY } from '../hints/hintsStorage';
import { PAGE_STATUS_ORDER, PAGE_STATUS_LABEL } from '../utils/pageStatus';
import { SEARCH_HINTS } from '../utils/searchHints';
import { HintsProvider } from '../contexts/HintsProvider';
import { useHints } from '../contexts/hintsContext';
import { resetSiteForTests } from '../services/site';

function setup() {
  const user = userEvent.setup();
  const { container } = render(
    <MemoryRouter>
      <Help />
    </MemoryRouter>,
  );
  return { user, container };
}

/** Пульт координатора: даёт из теста дёрнуть register/setVisible/learn для
 *  «download» и подглядеть, кого координатор считает активной подсказкой. Тот
 *  же приём, что в HintsProvider.test.tsx — приходится повторить здесь, а не
 *  подключить чужой Probe: он не экспортирован. */
function CoordinatorProbe() {
  const hints = useHints();
  if (!hints) return null;
  return (
    <div>
      <p data-testid="active">{hints.active?.id ?? 'нет'}</p>
      <button onClick={() => hints.register('download', 'пульт')}>рег</button>
      <button onClick={() => hints.setVisible('download', 'пульт', true)}>виден</button>
      <button onClick={() => hints.learn('download')}>усвоил</button>
    </div>
  );
}

beforeEach(() => {
  localStorage.clear();
  // Раздел архива спрашивает /api/static-archive; справке в этих тестах
  // достаточно, что архива нет.
  vi.spyOn(staticArchiveApi, 'get').mockImplementation(() =>
    Promise.reject(new Error('нет архива')),
  );
});
afterEach(() => vi.restoreAllMocks());

describe('страница справки', () => {
  // Ссылка из подвала ведёт на /help#offline: раздел обязан стоять сразу за
  // «Забрать с собой», а тот — называть его.
  it('ведёт из «Забрать с собой» в раздел об архиве сразу за ним', () => {
    setup();
    const download = document.getElementById('download')?.closest('section');
    const offline = document.getElementById('offline')?.closest('section');
    expect(offline).not.toBeNull();
    expect(download?.nextElementSibling).toBe(offline);
    expect(download?.querySelector('a[href="#offline"]')).not.toBeNull();
  });

  it('рассказывает, что это за место', () => {
    setup();
    expect(document.getElementById('about')).not.toBeNull();
  });

  // Хвостик «Подробнее» ведёт на /help#<идентификатор подсказки>. Раздел без
  // якоря — ссылка в никуда, и заметить это в браузере можно только случайно.
  it('у каждой подсказки есть раздел с её якорем', () => {
    setup();
    const missing = HINT_IDS.filter((id) => document.getElementById(id) === null);
    expect(missing, `нет разделов: ${missing.join(', ')}`).toEqual([]);
  });

  /*
   * Потоковое чтение — единственный способ читать работу насквозь, и вход в
   * него есть в двух местах сразу: ссылка «потоком» в содержании тома и
   * кнопка «Читать потоком» в шапке главы. Справка обязана назвать оба: по
   * одному найденному входу читатель не догадается о втором.
   */
  it('объясняет чтение потоком и называет оба входа в него', () => {
    setup();
    const section = document.getElementById('reading-stream');
    expect(section, 'раздела про чтение потоком нет').not.toBeNull();
    const prose = section?.parentElement?.textContent ?? '';
    expect(prose).toContain('потоком');
    expect(prose).toContain('Читать потоком');
  });

  /*
   * Приёмы запроса работают с первого дня (websearch_to_tsquery), но узнать о
   * них читателю было неоткуда: кавычки названы только в плейсхолдере поля и
   * в строке про пустую выдачу, а исключение и «или» — нигде. Раздел обязан
   * назвать каждый приём поимённо, и якорь #search обязан быть на месте: на
   * него ведёт ссылка со страницы поиска.
   */
  it('называет все приёмы поискового запроса и держит якорь #search', () => {
    setup();
    const section = document.getElementById('search');
    expect(section, 'раздела про поиск нет').not.toBeNull();
    const prose = section?.parentElement?.textContent ?? '';
    for (const sample of ['"что делать"', 'партия -меньшевики', 'Плеханов or Аксельрод', 'Ёлка']) {
      expect(prose, `не назван приём ${sample}`).toContain(sample);
    }
    // Порог назван словами: отказ сервера читатель встретит и без справки, а
    // объяснение должно быть там же, где остальные правила запроса.
    expect(prose).toMatch(/двух символов/);
  });

  // Справка и панель поиска обязаны показывать один и тот же список приёмов:
  // порознь они расходятся, справку правят словами, панель — вёрсткой.
  it('приёмы запроса в справке — те же, что в панели поиска', () => {
    setup();
    for (const hint of SEARCH_HINTS) {
      expect(screen.getByText(hint.query)).toBeInTheDocument();
    }
  });

  // Читатель встречает пропуск в нумерации страниц и должен узнать оттуда,
  // почему он там: якорь #withheld — цель ссылки из раздела «Обрез тома».
  it('объясняет, чего не хватает в некоторых томах, и держит якорь #withheld', () => {
    setup();
    const section = document.getElementById('withheld');
    expect(section, 'раздела про изъятое нет').not.toBeNull();
    const prose = section?.parentElement?.textContent ?? '';
    expect(prose).toContain('авторским правом');
    expect(prose).toContain('карточке');
  });

  it('расшифровывает все семь состояний страницы', () => {
    setup();
    for (const status of PAGE_STATUS_ORDER) {
      expect(screen.getByText(PAGE_STATUS_LABEL[status])).toBeInTheDocument();
    }
  });

  // Граница, которую читателю нужно понять: цитата уносит место, выгрузка —
  // том целиком. Раздел про цитирование обязан стоять сразу перед разделом
  // про выгрузку, а не где-то ещё в списке.
  it('раздел о цитировании стоит непосредственно перед разделом о выгрузке', () => {
    const { container } = setup();
    const ids = [...container.querySelectorAll('h2[id]')].map((h) => h.id);
    expect(ids.indexOf('cite')).toBe(ids.indexOf('download') - 1);
  });

  it('раздел называет оба числа подписи', () => {
    setup();
    const section = screen.getByRole('heading', { name: /Как сослаться/ }).closest('section')!;
    expect(section).toHaveTextContent('печатный номер страницы');
    expect(section).toHaveTextContent('б/н');
  });

  describe('поддержка — ссылка экземпляра', () => {
    function mockSupport(url: string) {
      vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify({ site_name: 'Читальня', support_url: url })),
      );
    }
    beforeEach(() => resetSiteForTests());
    afterEach(() => resetSiteForTests());

    it('говорит, как поддержать читальню, и держит якорь #support', async () => {
      mockSupport('https://example.org/donate');
      setup();
      const heading = await screen.findByRole('heading', { name: 'Поддержать читальню' });
      expect(heading.id).toBe('support');
      const link = heading.closest('section')!.querySelector('a');
      expect(link).toHaveAttribute('href', 'https://example.org/donate');
      expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });

    // Просьба о деньгах — после всего, что читальня даёт, а не в начале справки.
    it('раздел о поддержке стоит последним', async () => {
      mockSupport('https://example.org/donate');
      const { container } = setup();
      await screen.findByRole('heading', { name: 'Поддержать читальню' });
      const headings = [...container.querySelectorAll('h2')];
      expect(headings[headings.length - 1]?.id).toBe('support');
    });

    // Экземпляр без ссылки не просит денег и не оставляет ссылки в никуда.
    it('без ссылки экземпляра раздела нет', async () => {
      mockSupport('');
      setup();
      await new Promise((r) => setTimeout(r, 0));
      expect(screen.queryByRole('heading', { name: 'Поддержать читальню' })).toBeNull();
    });
  });

  it('объясняет, как спросить нейросеть', () => {
    setup();
    const heading = screen.getByRole('heading', { name: 'Спросить нейросеть' });
    expect(heading.id).toBe('ask-ai');
    const section = heading.closest('section')!;
    expect(section.textContent).toContain('номерам страниц');
    expect(section.textContent).toContain('понятия предметного указателя');
    expect(section.textContent).toContain('выберите подрубрику');
    expect(section.querySelector('a[href="#cite"]')).not.toBeNull();
  });

  // Обычную ссылку на главу nginx отдаёт готовым HTML только сборщикам
  // ChatGPT, Claude и Perplexity (карта $is_crawler); остальные чаты получат
  // пустую оболочку SPA. Работает везде только ссылка кнопки — на .md.
  it('не обещает, что обычная ссылка на главу работает в любом чате', () => {
    setup();
    const section = screen.getByRole('heading', { name: 'Спросить нейросеть' }).closest('section')!;
    expect(section.textContent).toContain(
      'В ChatGPT, Claude и Perplexity подойдёт и обычная ссылка на главу',
    );
    expect(section.textContent).not.toContain('Обычная ссылка на главу тоже подойдёт');
  });

  it('объясняет, как подключить нейросеть ко всей читальне', () => {
    setup();
    const heading = screen.getByRole('heading', { name: 'Спросить нейросеть по всей читальне' });
    expect(heading.id).toBe('mcp');
    const section = heading.closest('section')!;
    expect(section.textContent).toContain(`${window.location.origin}/mcp`);
    expect(section.textContent).toContain('claude mcp add --transport http');
    expect(section.querySelector('a[href="#ask-ai"]')).not.toBeNull();
    expect(section.querySelector('a[href="#cite"]')).not.toBeNull();
    expect(section.textContent).not.toMatch(/VPN/i);
  });

  it('объясняет, как подключить читалку к каталогу OPDS', () => {
    setup();
    const heading = screen.getByRole('heading', { name: 'Каталог для читалки' });
    expect(heading.id).toBe('opds');
    const section = heading.closest('section')!;
    expect(section.textContent).toContain(`${window.location.origin}/opds`);
    expect(section.textContent).toContain('целиком');
    const download = screen.getByRole('heading', { name: 'Забрать с собой' }).closest('section')!;
    expect(download.querySelector('a[href="#opds"]')).not.toBeNull();
  });

  it('из раздела о главе ведёт к разделу о всей читальне', () => {
    setup();
    const section = screen.getByRole('heading', { name: 'Спросить нейросеть' }).closest('section')!;
    expect(section.querySelector('a[href="#mcp"]')).not.toBeNull();
  });

  it('возвращает подсказки тому, кто от них отмахнулся', async () => {
    localStorage.setItem(HINTS_KEY, JSON.stringify({ download: { seen: 3, done: true } }));
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Показать подсказки заново' }));
    expect(localStorage.getItem(HINTS_KEY)).toBeNull();
  });

  // <Link> в балконе ведёт на /help#<id> через pushState, а к фрагменту после
  // pushState браузер сам не прокручивает (это делает только popstate) —
  // без явного эффекта на location.hash переход открывал бы страницу сверху.
  it('переход по /help#<id> прокручивает к разделу', () => {
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(function (
      this: Element,
    ) {
      void this;
    });
    render(
      <MemoryRouter initialEntries={['/help#outline-depth']}>
        <Help />
      </MemoryRouter>,
    );
    const target = document.getElementById('outline-depth');
    expect(spy).toHaveBeenCalledOnce();
    expect(spy.mock.contexts[0]).toBe(target);
    expect(spy).toHaveBeenCalledWith({ behavior: 'smooth' });
    spy.mockRestore();
  });

  // CSS scroll-behavior на JS-опцию не влияет — настройку читаем вручную,
  // тот же приём, что у кнопки «Наверх» в ScrollDock.
  it('при prefers-reduced-motion прокручивает к разделу мгновенно', () => {
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }) as unknown as typeof window.matchMedia;
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(function (
      this: Element,
    ) {
      void this;
    });
    render(
      <MemoryRouter initialEntries={['/help#outline-depth']}>
        <Help />
      </MemoryRouter>,
    );
    expect(spy).toHaveBeenCalledWith({ behavior: 'auto' });
    spy.mockRestore();
  });

  it('без хэша в адресе никуда не прокручивает', () => {
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView');
    setup();
    expect(spy).not.toHaveBeenCalled();
    spy.mockRestore();
  });

  // clearHints() очищает только localStorage. Координатор подсказок читает
  // его один раз, в инициализаторе состояния, поэтому без сброса его памяти
  // (HintsValue.reset()) усвоенная подсказка осталась бы «усвоенной» до
  // перезагрузки страницы — тест, проверяющий только localStorage (см. выше),
  // прошёл бы, даже если бы кнопка не звала reset() вовсе.
  it('возвращает подсказки и координатору, а не только localStorage', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <HintsProvider>
          <CoordinatorProbe />
          <Help />
        </HintsProvider>
      </MemoryRouter>,
    );

    await user.click(screen.getByText('рег'));
    await user.click(screen.getByText('виден'));
    expect(screen.getByTestId('active').textContent).toBe('download');

    await user.click(screen.getByText('усвоил'));
    expect(screen.getByTestId('active').textContent).toBe('нет');

    await user.click(screen.getByRole('button', { name: 'Показать подсказки заново' }));
    expect(screen.getByTestId('active').textContent).toBe('download');
  });
});

describe('раздел «Слушать»', () => {
  // Ссылка «Подробнее» из панели главы и раздела тома ведёт на этот якорь;
  // спека называет пять тем раздела.
  it('есть якорь audio и все пять тем', () => {
    const { container } = setup();
    const section = container.querySelector('#audio')?.closest('section');
    expect(section).not.toBeNull();
    const text = section?.textContent ?? '';
    expect(text).toMatch(/Silero/); // откуда звук
    expect(text).toMatch(/синтез/i); // почему синтез
    expect(text).toMatch(/примечания/); // что не звучит
    expect(text).toMatch(/VLC/); // как открыть плейлист
    expect(text).toMatch(/iOS/); // iOS старше 18.4
  });
});
