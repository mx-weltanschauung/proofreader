import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { suggestionsApi, pagesApi } from '../services/api';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { SuggestionQueue } from './SuggestionQueue';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const DEFAULT_PAGE = {
  id: 42,
  work_id: 7,
  page_number: 12,
  preview_path: 'works/7/pages/page_12.png',
  content_markdown: 'основа',
  status: 'вычитано_машиной',
  created_at: '',
  updated_at: '',
};

function renderQueueDetail(id: number) {
  vi.spyOn(pagesApi, 'get').mockReturnValue(ok(DEFAULT_PAGE) as never);
  return render(
    <ReadingPreferencesProvider>
      <MemoryRouter initialEntries={[`/suggestions/queue?id=${id}`]}>
        <Routes>
          <Route path="/suggestions/queue" element={<SuggestionQueue />} />
        </Routes>
      </MemoryRouter>
    </ReadingPreferencesProvider>,
  );
}

function renderQueueList() {
  return render(
    <ReadingPreferencesProvider>
      <MemoryRouter initialEntries={['/suggestions/queue']}>
        <Routes>
          <Route path="/suggestions/queue" element={<SuggestionQueue />} />
        </Routes>
      </MemoryRouter>
    </ReadingPreferencesProvider>,
  );
}

describe('SuggestionQueue', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('список', () => {
    it('показывает том, страницу, знак length_delta и метку «устарело»', async () => {
      vi.spyOn(suggestionsApi, 'queue').mockReturnValue(
        ok({
          items: [
            {
              id: 1,
              page_id: 42,
              work_id: 7,
              work_title: 'Том 8',
              page_number: 12,
              page_offset: 0,
              note: '',
              status: 'новое',
              stale: true,
              length_delta: 4,
              created_at: '2026-08-27T10:00:00Z',
            },
            {
              id: 2,
              page_id: 43,
              work_id: 7,
              work_title: 'Том 9',
              page_number: 5,
              page_offset: 0,
              note: '',
              status: 'новое',
              stale: false,
              length_delta: -3,
              created_at: '2026-08-27T11:00:00Z',
            },
          ],
          total: 2,
        }) as never,
      );

      renderQueueList();

      expect(await screen.findByText('Том 8')).toBeInTheDocument();
      expect(screen.getByText('Том 9')).toBeInTheDocument();
      expect(screen.getByText('+4')).toBeInTheDocument();
      expect(screen.getByText('-3')).toBeInTheDocument();
      expect(screen.getByText('устарело')).toBeInTheDocument();
    });

    it('запрашивает очередь по статусу «новое» по умолчанию', async () => {
      const queue = vi
        .spyOn(suggestionsApi, 'queue')
        .mockReturnValue(ok({ items: [], total: 0 }) as never);

      renderQueueList();

      await waitFor(() => {
        expect(queue).toHaveBeenCalledWith('новое', 20, 0);
      });
    });

    it('пустая очередь показывает заглушку, а не пустой список', async () => {
      vi.spyOn(suggestionsApi, 'queue').mockReturnValue(ok({ items: [], total: 0 }) as never);

      renderQueueList();

      expect(await screen.findByText(/пока нет предложений/i)).toBeInTheDocument();
    });

    it('смена фильтра сбрасывает смещение в начало', async () => {
      // Без сброса редактор, ушедший на вторую страницу «Новых», переключился
      // бы на «Принятые» и увидел бы пустой хвост непустой выборки — offset
      // 20 мог оказаться за пределами меньшего списка.
      const user = userEvent.setup();
      const queue = vi.spyOn(suggestionsApi, 'queue').mockReturnValue(
        ok({
          items: [
            {
              id: 1,
              page_id: 42,
              work_id: 7,
              work_title: 'Том 8',
              page_number: 12,
              page_offset: 0,
              note: '',
              status: 'новое',
              stale: false,
              length_delta: 1,
              created_at: '2026-08-27T10:00:00Z',
            },
          ],
          total: 50,
        }) as never,
      );

      renderQueueList();
      await screen.findByText('Том 8');

      await user.click(screen.getByRole('button', { name: /дальше/i }));
      await waitFor(() => expect(queue).toHaveBeenCalledWith('новое', 20, 20));

      await user.selectOptions(screen.getByLabelText(/статус/i), 'принято');
      await waitFor(() => expect(queue).toHaveBeenLastCalledWith('принято', 20, 0));
    });
  });

  describe('разбор одного предложения', () => {
    it('на устаревшем предложении предзаполняет ТЕКУЩИЙ текст, а не предложенный', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 3,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: true,
          length_delta: 4,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'текст после прогона машинной вычитки',
        }) as never,
      );

      renderQueueDetail(3);

      // За время ожидания по полосе мог пройти прогон стадии E, и текущий текст
      // старший. Предзаполнить предложенным — значит стереть прогон одним кликом.
      expect(
        await screen.findByDisplayValue('текст после прогона машинной вычитки'),
      ).toBeInTheDocument();
      expect(screen.queryByDisplayValue('текст читателя')).not.toBeInTheDocument();
    });

    it('на свежем предложении предзаполняет предложенное', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 4,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'основа',
        }) as never,
      );

      renderQueueDetail(4);
      expect(await screen.findByDisplayValue('текст читателя')).toBeInTheDocument();
    });

    it('«подставить предложенное» — кнопка, не значение по умолчанию', async () => {
      const user = userEvent.setup();
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 3,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: true,
          length_delta: 4,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'текст после прогона машинной вычитки',
        }) as never,
      );

      renderQueueDetail(3);
      await screen.findByDisplayValue('текст после прогона машинной вычитки');

      await user.click(screen.getByRole('button', { name: /подставить предложенное/i }));

      expect(await screen.findByDisplayValue('текст читателя')).toBeInTheDocument();
    });

    it('записка читателя показана над диффом', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 5,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: 'Тут явная опечатка в третьей строке',
          status: 'новое',
          stale: false,
          length_delta: 0,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'основа исправленная',
          current_markdown: 'основа',
        }) as never,
      );

      renderQueueDetail(5);
      expect(await screen.findByText('Тут явная опечатка в третьей строке')).toBeInTheDocument();
    });

    it('«Принять» отправляет текст из поля', async () => {
      const user = userEvent.setup();
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 6,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'основа',
        }) as never,
      );
      const accept = vi
        .spyOn(suggestionsApi, 'accept')
        .mockReturnValue(ok({ id: 6, status: 'принято' }) as never);

      renderQueueDetail(6);
      await screen.findByDisplayValue('текст читателя');

      await user.click(screen.getByRole('button', { name: /^принять$/i }));

      await waitFor(() => {
        expect(accept).toHaveBeenCalledWith(6, 'текст читателя', '');
      });
    });

    it('«Отклонить» заблокирован, пока не выбрана причина', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 7,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'основа',
        }) as never,
      );

      renderQueueDetail(7);
      await screen.findByDisplayValue('текст читателя');

      expect(screen.getByRole('button', { name: /^отклонить$/i })).toBeDisabled();
    });

    it('«Отклонить» шлёт выбранную причину из трёх', async () => {
      const user = userEvent.setup();
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 8,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'основа',
        }) as never,
      );
      const reject = vi
        .spyOn(suggestionsApi, 'reject')
        .mockReturnValue(ok({ id: 8, status: 'отклонено' }) as never);

      renderQueueDetail(8);
      await screen.findByDisplayValue('текст читателя');

      await user.selectOptions(screen.getByLabelText(/причина отказа/i), 'не_по_теме');
      await user.click(screen.getByRole('button', { name: /^отклонить$/i }));

      await waitFor(() => {
        expect(reject).toHaveBeenCalledWith(8, 'не_по_теме');
      });
    });

    it('409 при принятии показывает сообщение и возвращает к списку', async () => {
      const user = userEvent.setup();
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 9,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст читателя',
          current_markdown: 'основа',
        }) as never,
      );
      vi.spyOn(suggestionsApi, 'accept').mockRejectedValue({
        response: { status: 409, data: { message: 'Предложение уже разобрано' } },
      });
      const queue = vi
        .spyOn(suggestionsApi, 'queue')
        .mockReturnValue(ok({ items: [], total: 0 }) as never);

      renderQueueDetail(9);
      await screen.findByDisplayValue('текст читателя');

      await user.click(screen.getByRole('button', { name: /^принять$/i }));

      // Сообщение о конфликте уходит тостом (react-hot-toast, как в
      // UsersList) — сторожевого монтирования <Toaster/> в тестах нигде в
      // кодовой базе нет, поэтому проверяем наблюдаемое поведение: экран
      // вернулся к списку, а значит список запросили заново.
      await waitFor(() => {
        expect(screen.queryByDisplayValue('текст читателя')).not.toBeInTheDocument();
      });
      expect(queue).toHaveBeenCalled();
    });

    it('предпросмотр итогового текста санитизирует чужой HTML — <script> из текста читателя не живой узел', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(
        ok({
          id: 10,
          page_id: 42,
          work_id: 7,
          work_title: 'Том 8',
          page_number: 12,
          page_offset: 0,
          note: '',
          status: 'новое',
          stale: false,
          length_delta: 1,
          created_at: '2026-08-27T10:00:00Z',
          base_markdown: 'основа',
          proposed_markdown: 'текст <script>window.__pwned = true</script> от читателя',
          current_markdown: 'основа',
        }) as never,
      );

      const { container } = renderQueueDetail(10);
      await screen.findByDisplayValue('текст <script>window.__pwned = true</script> от читателя');

      await waitFor(() => {
        expect(container.querySelector('.wmde-markdown')).toBeInTheDocument();
      });
      expect(container.querySelector('.wmde-markdown script')).toBeNull();
      expect((window as unknown as { __pwned?: boolean }).__pwned).toBeUndefined();
    });

    // Разобранное предложение сервер 409-т на повторный accept/reject — эти
    // кнопки и редактируемое поле должны исчезать, а не оставаться рабочими
    // элементами, которые лишь позже упрутся в ошибку.
    describe('разобранное предложение — не рабочее место', () => {
      it('неразобранное («новое») по-прежнему показывает поле и обе кнопки', async () => {
        vi.spyOn(suggestionsApi, 'get').mockReturnValue(
          ok({
            id: 11,
            page_id: 42,
            work_id: 7,
            work_title: 'Том 8',
            page_number: 12,
            page_offset: 0,
            note: '',
            status: 'новое',
            stale: false,
            length_delta: 1,
            created_at: '2026-08-27T10:00:00Z',
            base_markdown: 'основа',
            proposed_markdown: 'текст читателя',
            current_markdown: 'основа',
          }) as never,
        );

        renderQueueDetail(11);
        await screen.findByDisplayValue('текст читателя');

        expect(screen.getByRole('button', { name: /^принять$/i })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /^отклонить$/i })).toBeInTheDocument();
      });

      it('принятое не показывает поле и кнопки, называет решение и время, ведёт на применённую полосу', async () => {
        vi.spyOn(suggestionsApi, 'get').mockReturnValue(
          ok({
            id: 12,
            page_id: 42,
            work_id: 7,
            work_title: 'Том 8',
            page_number: 12,
            page_offset: 0,
            note: '',
            status: 'принято',
            stale: false,
            length_delta: 1,
            created_at: '2026-08-27T10:00:00Z',
            resolved_at: '2026-08-27T12:00:00Z',
            base_markdown: 'основа',
            proposed_markdown: 'текст читателя',
            current_markdown: 'основа',
          }) as never,
        );

        renderQueueDetail(12);
        expect(await screen.findByText(/Принято/)).toBeInTheDocument();

        expect(screen.queryByDisplayValue('текст читателя')).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /^принять$/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /^отклонить$/i })).not.toBeInTheDocument();

        // Дифф предложения читателя остаётся видимым — это история, что
        // просили, а не свидетельство того, что применили дословно.
        expect(screen.getByText('текст читателя')).toBeInTheDocument();

        expect(screen.getByRole('link', { name: /применённый текст/i })).toHaveAttribute(
          'href',
          '/works/7/pages/12',
        );
      });

      it('отклонённое называет причину человеческими словами, а не машинным значением', async () => {
        vi.spyOn(suggestionsApi, 'get').mockReturnValue(
          ok({
            id: 13,
            page_id: 42,
            work_id: 7,
            work_title: 'Том 8',
            page_number: 12,
            page_offset: 0,
            note: '',
            status: 'отклонено',
            reject_reason: 'так_в_оригинале',
            stale: false,
            length_delta: 1,
            created_at: '2026-08-27T10:00:00Z',
            resolved_at: '2026-08-27T12:00:00Z',
            base_markdown: 'основа',
            proposed_markdown: 'текст читателя',
            current_markdown: 'основа',
          }) as never,
        );

        renderQueueDetail(13);
        expect(await screen.findByText(/Отклонено/)).toBeInTheDocument();
        expect(screen.getByText(/Так в оригинале/)).toBeInTheDocument();
        expect(screen.queryByText(/так_в_оригинале/)).not.toBeInTheDocument();

        expect(screen.queryByDisplayValue('текст читателя')).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /^принять$/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /^отклонить$/i })).not.toBeInTheDocument();
        expect(screen.getByText('текст читателя')).toBeInTheDocument();
      });
    });
  });

  describe('удаление отклонённых', () => {
    function decided(status: 'новое' | 'принято' | 'отклонено') {
      return {
        id: 13,
        page_id: 42,
        work_id: 7,
        work_title: 'Том 8',
        page_number: 12,
        page_offset: 0,
        note: '',
        status,
        ...(status === 'отклонено' ? { reject_reason: 'не_по_теме' } : {}),
        stale: false,
        length_delta: 1,
        created_at: '2026-08-27T10:00:00Z',
        resolved_at: status === 'новое' ? undefined : '2026-08-27T12:00:00Z',
        base_markdown: 'основа',
        proposed_markdown: 'текст читателя',
        current_markdown: 'основа',
      };
    }

    function rejectedRow(id: number, title: string) {
      return {
        id,
        page_id: 42,
        work_id: 7,
        work_title: title,
        page_number: 12,
        page_offset: 0,
        note: '',
        status: 'отклонено' as const,
        reject_reason: 'не_по_теме' as const,
        stale: false,
        length_delta: 1,
        created_at: '2026-08-27T10:00:00Z',
        resolved_at: '2026-08-27T12:00:00Z',
      };
    }

    it('кнопка удаления есть у отклонённого', async () => {
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(ok(decided('отклонено')) as never);

      renderQueueDetail(13);
      await screen.findByText(/Отклонено/);

      expect(screen.getByRole('button', { name: /удалить/i })).toBeInTheDocument();
    });

    it.each(['новое', 'принято'] as const)(
      'кнопки удаления нет у предложения со статусом «%s»',
      async (status) => {
        // Принятое — след того, откуда на полосе взялась правка; новое ещё
        // ждёт разбора. Сервер откажет и так, но кнопки быть не должно.
        vi.spyOn(suggestionsApi, 'get').mockReturnValue(ok(decided(status)) as never);

        renderQueueDetail(13);
        await screen.findByText('Том 8', { exact: false });

        expect(screen.queryByRole('button', { name: /удалить/i })).not.toBeInTheDocument();
      },
    );

    it('удаление спрашивает подтверждение и шлёт запрос по номеру', async () => {
      const user = userEvent.setup();
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(ok(decided('отклонено')) as never);
      vi.spyOn(suggestionsApi, 'queue').mockReturnValue(ok({ items: [], total: 0 }) as never);
      const remove = vi
        .spyOn(suggestionsApi, 'remove')
        .mockReturnValue(ok(undefined as never) as never);

      renderQueueDetail(13);
      await screen.findByText(/Отклонено/);
      await user.click(screen.getByRole('button', { name: /удалить/i }));

      await waitFor(() => expect(remove).toHaveBeenCalledWith(13));
      // После удаления смотреть не на что: экран возвращается к очереди.
      expect(await screen.findByText(/пока нет предложений/i)).toBeInTheDocument();
    });

    it('отказ в подтверждении ничего не удаляет', async () => {
      const user = userEvent.setup();
      vi.spyOn(window, 'confirm').mockReturnValue(false);
      vi.spyOn(suggestionsApi, 'get').mockReturnValue(ok(decided('отклонено')) as never);
      const remove = vi
        .spyOn(suggestionsApi, 'remove')
        .mockReturnValue(ok(undefined as never) as never);

      renderQueueDetail(13);
      await screen.findByText(/Отклонено/);
      await user.click(screen.getByRole('button', { name: /удалить/i }));

      expect(remove).not.toHaveBeenCalled();
    });

    it('массовая чистка предлагается только на отклонённых', async () => {
      const user = userEvent.setup();
      vi.spyOn(suggestionsApi, 'queue').mockReturnValue(
        ok({ items: [rejectedRow(1, 'Том 8')], total: 1 }) as never,
      );

      renderQueueList();
      await screen.findByText('Том 8');
      expect(screen.queryByRole('button', { name: /удалить все/i })).not.toBeInTheDocument();

      await user.selectOptions(screen.getByLabelText(/статус/i), 'отклонено');

      expect(await screen.findByRole('button', { name: /удалить все/i })).toBeInTheDocument();
    });

    it('массовая чистка шлёт запрос и перечитывает список', async () => {
      const user = userEvent.setup();
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      const queue = vi
        .spyOn(suggestionsApi, 'queue')
        .mockReturnValue(ok({ items: [rejectedRow(1, 'Том 8')], total: 1 }) as never);
      const purge = vi
        .spyOn(suggestionsApi, 'purgeRejected')
        .mockReturnValue(ok({ deleted: 1 }) as never);

      renderQueueList();
      await screen.findByText('Том 8');
      await user.selectOptions(screen.getByLabelText(/статус/i), 'отклонено');

      const callsBefore = queue.mock.calls.length;
      await user.click(await screen.findByRole('button', { name: /удалить все/i }));

      await waitFor(() => expect(purge).toHaveBeenCalled());
      // Список после чистки обязан перечитаться — иначе редактор смотрит на
      // строки, которых в базе уже нет, и жмёт по ним в 409.
      await waitFor(() => expect(queue.mock.calls.length).toBeGreaterThan(callsBefore));
    });

    it('строку отклонённого можно снести прямо из списка', async () => {
      const user = userEvent.setup();
      vi.spyOn(window, 'confirm').mockReturnValue(true);
      vi.spyOn(suggestionsApi, 'queue').mockReturnValue(
        ok({ items: [rejectedRow(5, 'Том 8')], total: 1 }) as never,
      );
      const remove = vi
        .spyOn(suggestionsApi, 'remove')
        .mockReturnValue(ok(undefined as never) as never);

      renderQueueList();
      await screen.findByText('Том 8');
      await user.selectOptions(screen.getByLabelText(/статус/i), 'отклонено');

      await user.click(await screen.findByRole('button', { name: /удалить предложение/i }));

      await waitFor(() => expect(remove).toHaveBeenCalledWith(5));
    });
  });
});
