import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { conceptsApi } from '../services/api';
import type { ConceptSummary } from '../types';
import { ConceptList, PAGE_SIZE } from './ConceptList';
import { SITE_NAME } from '../hooks/useDocumentTitle';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function concept(
  id: number,
  title: string,
  kind: 'article' | 'redirect' = 'article',
): ConceptSummary {
  return {
    id,
    title,
    slug: `slug-${id}`,
    sort_key: title.toLowerCase(),
    kind,
    created_at: '',
    updated_at: '',
  };
}

function renderList(path = '/concepts') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ConceptList />
    </MemoryRouter>,
  );
}

describe('ConceptList', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('показывает понятия ссылками на их страницы', async () => {
    vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([concept(1, 'Абстрактный труд')]));

    renderList();

    expect(await screen.findByRole('link', { name: 'Абстрактный труд' })).toHaveAttribute(
      'href',
      '/concepts/slug-1',
    );
  });

  // F4 итогового ревью: заголовок вкладки не был проложен вовсе — читатель
  // видел «Читальня» на индексируемой странице, тогда как сервер
  // (internal/seo/render_index.go, ConceptList) отдаёт краулеру «Предметный
  // указатель — Читальня».
  it('ставит заголовок вкладки, совпадающий с серверным', async () => {
    document.title = SITE_NAME;
    vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([concept(1, 'Абстрактный труд')]));

    renderList();

    await screen.findByRole('link', { name: 'Абстрактный труд' });
    // Заголовок ставится эффектом, а проверка идёт сразу за ожиданием
    // разметки — под нагрузкой полного прогона эффект успевает не всегда.
    // Синхронная проверка здесь давала нестабильный провал примерно раз
    // на два прогона: ждём сам заголовок, а не разметку рядом с ним.
    await waitFor(() => {
      expect(document.title).toBe(`Предметный указатель — ${SITE_NAME}`);
    });
  });

  it('помечает отсылочные статьи', async () => {
    vi.spyOn(conceptsApi, 'list').mockImplementation(() =>
      ok([concept(2, 'Абсолютное и относительное', 'redirect')]),
    );

    renderList();

    expect(await screen.findByText('см.')).toBeInTheDocument();
  });

  it('шлёт букву в нижнем регистре', async () => {
    // sort_key на сервере в нижнем регистре, а фильтр — LIKE letter || '%'.
    const list = vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([]));

    renderList();
    await userEvent.click(await screen.findByRole('button', { name: 'А' }));

    await waitFor(() =>
      expect(list).toHaveBeenLastCalledWith({ q: '', letter: 'а', limit: PAGE_SIZE, offset: 0 }),
    );
  });

  it('поиск снимает выбранную букву', async () => {
    // Сервер складывает q и letter через AND: «искал внутри буквы Б и не
    // нашёл» — путаница на пустом месте.
    const list = vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([]));

    renderList('/concepts?letter=б');
    await userEvent.type(await screen.findByRole('searchbox'), 'абстр');

    await waitFor(() =>
      expect(list).toHaveBeenLastCalledWith({
        q: 'абстр',
        letter: '',
        limit: PAGE_SIZE,
        offset: 0,
      }),
    );
  });

  it('буква очищает строку поиска', async () => {
    const list = vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([]));

    renderList('/concepts?q=абстр');
    await userEvent.click(await screen.findByRole('button', { name: 'В' }));

    await waitFor(() =>
      expect(list).toHaveBeenLastCalledWith({ q: '', letter: 'в', limit: PAGE_SIZE, offset: 0 }),
    );
  });

  it('«Показать ещё» дозагружает и пропадает на коротком ответе', async () => {
    const full = Array.from({ length: PAGE_SIZE }, (_, i) => concept(i + 1, `Понятие ${i + 1}`));
    const list = vi
      .spyOn(conceptsApi, 'list')
      .mockImplementationOnce(() => ok(full))
      .mockImplementationOnce(() => ok([concept(999, 'Хвост')]));

    renderList();

    await userEvent.click(await screen.findByRole('button', { name: 'Показать ещё' }));

    await waitFor(() => expect(screen.getByRole('link', { name: 'Хвост' })).toBeInTheDocument());
    expect(list).toHaveBeenLastCalledWith({
      q: '',
      letter: '',
      limit: PAGE_SIZE,
      offset: PAGE_SIZE,
    });
    // Ответ короче лимита — дальше страниц нет.
    expect(screen.queryByRole('button', { name: 'Показать ещё' })).not.toBeInTheDocument();
  });

  it('переживает null вместо пустого списка', async () => {
    vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok(null));

    renderList();

    expect(await screen.findByText(/Ничего не найдено/)).toBeInTheDocument();
  });

  it('клик по букве отменяет отложенную отправку недопечатанного текста', async () => {
    // Синхронизация draft ← query видит только смену query, а клик по
    // букве меняет letter — draft остаётся старым, и уже взведённый таймер
    // дебаунса (он не знает про letter) переживает клик. Без явного сброса
    // draft в обработчике клика он через 250 мс отправит q со старым
    // текстом и затрёт только что выбранную букву.
    const list = vi.spyOn(conceptsApi, 'list').mockImplementation(() => ok([]));

    renderList();
    await userEvent.type(await screen.findByRole('searchbox'), 'абц');
    await userEvent.click(await screen.findByRole('button', { name: 'В' }));

    await waitFor(() =>
      expect(list).toHaveBeenLastCalledWith({ q: '', letter: 'в', limit: PAGE_SIZE, offset: 0 }),
    );

    // Ждём дольше дебаунса: если таймер выжил, он допишет q поверх буквы.
    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(list).toHaveBeenLastCalledWith({ q: '', letter: 'в', limit: PAGE_SIZE, offset: 0 });
  });

  it('«Показать ещё» не дописывает устаревший ответ после смены буквы', async () => {
    // У основного эффекта загрузки есть cancelled-флаг на случай, если
    // query/letter сменятся раньше ответа. У loadMore — обработчика клика,
    // а не эффекта — такой защиты не было: его ответ мог дописаться в
    // список уже другой буквы через setConcepts((prev) => [...prev, ...]).
    const full = Array.from({ length: PAGE_SIZE }, (_, i) => concept(i + 1, `Понятие ${i + 1}`));
    let resolveStaleLoadMore: (page: ConceptSummary[]) => void = () => {};
    const stalePage = new Promise<ConceptSummary[]>((resolve) => {
      resolveStaleLoadMore = resolve;
    });

    vi.spyOn(conceptsApi, 'list')
      .mockImplementationOnce(() => ok(full)) // начальная загрузка, буква ''
      .mockImplementationOnce(() =>
        stalePage.then((page) => ({ data: page }) as AxiosResponse<ConceptSummary[]>),
      ) // «Показать ещё» зависает
      .mockImplementationOnce(() => ok([concept(500, 'Свежее понятие')])); // переключение на букву «В»

    renderList();

    await userEvent.click(await screen.findByRole('button', { name: 'Показать ещё' }));
    await userEvent.click(await screen.findByRole('button', { name: 'В' }));

    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'Свежее понятие' })).toBeInTheDocument(),
    );

    // Досылаем зависший ответ «Показать ещё» — он относится к старому
    // запросу (буква ''), а не к текущему (буква «В»), и не должен попасть
    // в список.
    await act(async () => {
      resolveStaleLoadMore([concept(999, 'Хвост')]);
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(screen.queryByRole('link', { name: 'Хвост' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Свежее понятие' })).toBeInTheDocument();
  });
});
