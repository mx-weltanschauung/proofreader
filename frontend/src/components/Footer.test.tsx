import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Footer } from './Footer';
import { resetSiteForTests } from '../services/site';

const TELEGRAM_URL = 'https://t.me/example_channel';
const BOOSTY_URL = 'https://example.org/donate';

/** Экземпляр: /api/site отдаёт siteJSON, /legal.html — файл (или 404). */
function mockInstance(siteJSON: object, legal: boolean) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input);
    if (url.endsWith('/api/site')) return new Response(JSON.stringify(siteJSON));
    if (url.endsWith('/legal.html') && legal) {
      return new Response('<h1>x</h1>', { headers: { 'x-instance-file': '1' } });
    }
    return new Response('', { status: 404 });
  });
}

const FULL_SITE = {
  site_name: 'Тестовая',
  site_description: 'Тестовое собрание.',
  support_url: BOOSTY_URL,
  channel_url: TELEGRAM_URL,
  age_rating: '18+',
};

/** Подвал рисуется сразу с умолчанием; ждём, пока придут сведения экземпляра. */
async function renderFooter(initialEntries: string[] = ['/']) {
  const view = render(
    <MemoryRouter initialEntries={initialEntries}>
      <Footer />
    </MemoryRouter>,
  );
  await screen.findByRole('heading', { name: 'Тестовая' });
  return view;
}

afterEach(() => {
  vi.restoreAllMocks();
  resetSiteForTests();
});

describe('подвал пустого экземпляра', () => {
  // Без настроек у подвала нет ни канала, ни пожертвований, ни маркировки,
  // ни правовой страницы — и нет ссылок в никуда на их месте.
  it('не показывает ссылок, которых экземпляр не задал', async () => {
    mockInstance({}, false);
    const { container } = render(
      <MemoryRouter>
        <Footer />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Читальня' })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText(/Телеграм-канал|Канал новостей/)).toBeNull();
    expect(screen.queryByText('Поддержать читальню')).toBeNull();
    expect(screen.queryByText('Правовая информация')).toBeNull();
    expect(container.querySelector('.age-badge')).toBeNull();
  });
});

describe('подвал', () => {
  beforeEach(() => mockInstance(FULL_SITE, true));

  it('ведёт в справку живыми ссылками', async () => {
    await renderFooter();
    expect(screen.getByRole('link', { name: 'Справка' })).toHaveAttribute('href', '/help');
    expect(screen.getByRole('link', { name: 'О проекте' })).toHaveAttribute('href', '/help#about');
  });

  // Три ссылки вида href="#about" вели в никуда и не выдавали себя ничем.
  it('не оставляет ссылок в никуда', async () => {
    const { container } = await renderFooter();
    const dead = [...container.querySelectorAll('a')].filter((a) =>
      (a.getAttribute('href') ?? '').startsWith('#'),
    );
    expect(dead.map((a) => a.textContent)).toEqual([]);
  });

  // Подвал из четырёх блоков без заголовков читалка с экрана выдаёт сплошной
  // кашей ссылок: заголовки колонок обязаны быть заголовками.
  it('разбит на озаглавленные колонки', async () => {
    await renderFooter();
    expect(screen.getByRole('heading', { name: 'Тестовая' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Каталог' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Контакты' })).toBeInTheDocument();
  });

  // Проверяются адреса, но не существование маршрутов: ссылка на /editions
  // прошла эту проверку и молча улетала на главную через catch-all, потому
  // что списочной страницы изданий в приложении нет.
  it('ведёт в каталог', async () => {
    await renderFooter();
    expect(screen.getByRole('link', { name: 'Указатель' })).toHaveAttribute('href', '/concepts');
    expect(screen.getByRole('link', { name: 'Подборки' })).toHaveAttribute('href', '/collections');
    expect(screen.getByRole('link', { name: 'Тестовая целиком' })).toHaveAttribute(
      'href',
      '/help#offline',
    );
  });

  it('даёт контакты: канал и форму', async () => {
    await renderFooter();
    const channel = screen.getByRole('link', { name: /телеграм/i });
    expect(channel).toHaveAttribute('href', TELEGRAM_URL);
    expect(channel).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByRole('link', { name: 'Написать нам' })).toHaveAttribute(
      'href',
      expect.stringMatching(/^\/feedback/),
    );
  });

  // source_path не заполнялся никогда: подвал вёл на форму голой ссылкой
  // «/feedback», а Feedback.tsx читает путь из ?from=. Без него письмо
  // приходит без единого указания, о какой странице речь.
  it('кладёт в ссылку на форму путь, откуда её открыли', async () => {
    await renderFooter(['/works/16/pages/412']);
    expect(screen.getByRole('link', { name: 'Написать нам' })).toHaveAttribute(
      'href',
      '/feedback?from=%2Fworks%2F16%2Fpages%2F412',
    );
  });

  // Маркировка добровольная: 436-ФЗ выводит из-под себя научную продукцию
  // (ст. 1 ч. 2 п. 1) и продукцию значительной культурной ценности (п. 3), а
  // читальня к тому же не СМИ. Знак стоит по решению владельца — и раз стоит,
  // он обязан быть на каждой странице и иметь внятное имя для скринридера.
  it('показывает возрастную маркировку и ведёт с неё на разъяснение', async () => {
    await renderFooter();
    const badge = screen.getByRole('link', { name: 'для читателей 18 лет и старше' });
    expect(badge).toHaveTextContent('18+');
    expect(badge).toHaveAttribute('href', '/legal#age');
  });

  it('ведёт на страницу пожертвований', async () => {
    await renderFooter();
    const donate = screen.getByRole('link', { name: 'Поддержать читальню' });
    expect(donate).toHaveAttribute('href', BOOSTY_URL);
    expect(donate).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('ведёт на правовую информацию', async () => {
    await renderFooter();
    expect(screen.getByRole('link', { name: 'Правовая информация' })).toHaveAttribute(
      'href',
      '/legal',
    );
  });

  it('не дублирует ?from=, если форма открыта уже с самой формы', async () => {
    await renderFooter(['/feedback']);
    expect(screen.getByRole('link', { name: 'Написать нам' })).toHaveAttribute('href', '/feedback');
  });

  // Вход сотрудника — неприметной ссылкой в подвале, отдельно от читательской
  // /join: почтовый логин ошибается на опечатке и не должен стоять рядом с
  // формой, где несуществующее имя — это регистрация.
  it('даёт сотруднику неприметный вход по почте', async () => {
    await renderFooter();
    expect(screen.getByRole('link', { name: 'Вход для редакторов' })).toHaveAttribute(
      'href',
      '/login',
    );
  });
});
