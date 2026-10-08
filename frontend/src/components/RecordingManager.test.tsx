import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AxiosError, type AxiosResponse } from 'axios';
import { usePlayer } from '../audio/playerStore';
import { audioApi } from '../services/api';
import type { AudioRecording, RecordingUpload } from '../types';
import { RecordingManager } from './RecordingManager';

vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as AxiosResponse<T>);
}

function rec(id: number, position: number, reader = ''): AudioRecording {
  return {
    id,
    work_id: 4,
    chapter_id: 223,
    chapter_title: 'Глава',
    position,
    reader,
    content_type: 'audio/mpeg',
    bytes: 3,
    duration_ms: 1000,
    created_at: '',
    url: `/api/audio/rec/${id}`,
  };
}

function file(name: string) {
  return new File(['abc'], name);
}

function deps(
  overrides: Partial<{ put: ReturnType<typeof vi.fn>; probe: ReturnType<typeof vi.fn> }> = {},
) {
  return {
    put: overrides.put ?? vi.fn().mockResolvedValue(undefined),
    probe: overrides.probe ?? vi.fn().mockResolvedValue(62.5),
  };
}

afterEach(() => vi.restoreAllMocks());

describe('RecordingManager', () => {
  it('прикрепляет несколько файлов по одному: ссылка, PUT, регистрация с «Читает»', async () => {
    const upload = vi
      .spyOn(audioApi, 'recordingUploadURL')
      .mockImplementation(() =>
        ok<RecordingUpload>({ key: 'k', url: 'https://s3/k', content_type: 'audio/mpeg' }),
      );
    const register = vi
      .spyOn(audioApi, 'registerRecording')
      .mockImplementation(() => ok(rec(1, 1)));
    const d = deps();
    const onChanged = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={onChanged}
        deps={d}
      />,
    );
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Кто читает (для прикрепляемых файлов)' }),
      'Иванов',
    );
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [
      file('1.mp3'),
      file('2.mp3'),
    ]);
    await waitFor(() => expect(register).toHaveBeenCalledTimes(2));
    expect(upload).toHaveBeenCalledWith(4, 223, { content_type: 'audio/mpeg', bytes: 3 });
    expect(d.put).toHaveBeenCalledWith(
      'https://s3/k',
      expect.any(File),
      'audio/mpeg',
      expect.any(Function),
    );
    expect(register).toHaveBeenCalledWith(4, 223, {
      key: 'k',
      content_type: 'audio/mpeg',
      bytes: 3,
      duration_ms: 62500,
      reader: 'Иванов',
    });
    expect(onChanged).toHaveBeenCalled();
  });

  // Review Focus 2: чужой формат и неразборчивая длительность — отказ по
  // файлу до всякой заливки; соседний файл едет.
  it('чужой формат и неопределимая длительность — отказ по файлу, соседи едут', async () => {
    const upload = vi
      .spyOn(audioApi, 'recordingUploadURL')
      .mockImplementation(() =>
        ok<RecordingUpload>({ key: 'k', url: 'u', content_type: 'audio/mpeg' }),
      );
    vi.spyOn(audioApi, 'registerRecording').mockImplementation(() => ok(rec(1, 1)));
    const probe = vi
      .fn()
      .mockRejectedValueOnce(new Error('браузер не определил длительность файла'))
      .mockResolvedValue(10);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={vi.fn()}
        deps={deps({ probe })}
      />,
    );
    await userEvent.upload(
      screen.getByLabelText('Прикрепить запись'),
      [file('a.wav'), file('b.mp3'), file('c.mp3')],
      {
        applyAccept: false,
      },
    );
    expect(await screen.findByText(/a\.wav: формат не поддерживается/)).toBeInTheDocument();
    expect(
      await screen.findByText(/b\.mp3: браузер не определил длительность файла/),
    ).toBeInTheDocument();
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1)); // только c.mp3
  });

  // Review Focus 3: оборванная заливка — без регистрации, следующий файл едет.
  it('оборванная заливка — без регистрации, следующий файл едет', async () => {
    vi.spyOn(audioApi, 'recordingUploadURL').mockImplementation(() =>
      ok<RecordingUpload>({ key: 'k', url: 'u', content_type: 'audio/mpeg' }),
    );
    const register = vi
      .spyOn(audioApi, 'registerRecording')
      .mockImplementation(() => ok(rec(1, 1)));
    const put = vi
      .fn()
      .mockRejectedValueOnce(new Error('заливка оборвалась'))
      .mockResolvedValue(undefined);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={vi.fn()}
        deps={deps({ put })}
      />,
    );
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [
      file('a.mp3'),
      file('b.mp3'),
    ]);
    expect(await screen.findByText(/a\.mp3: заливка оборвалась/)).toBeInTheDocument();
    await waitFor(() => expect(register).toHaveBeenCalledTimes(1));
  });

  // Тикет 07, п. 2: axios ставит response только при ответе сервера, поэтому
  // обрыв связи и таймаут печатали «Network Error» / «timeout of … exceeded».
  it('сбой связи с сервером — по-русски, а не текст axios', async () => {
    vi.spyOn(audioApi, 'recordingUploadURL')
      .mockImplementationOnce(() => Promise.reject(new AxiosError('Network Error', 'ERR_NETWORK')))
      .mockImplementation(() =>
        ok<RecordingUpload>({ key: 'k', url: 'u', content_type: 'audio/mpeg' }),
      );
    vi.spyOn(audioApi, 'registerRecording').mockImplementation(() =>
      Promise.reject(new AxiosError('timeout of 30000ms exceeded', 'ECONNABORTED')),
    );
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={vi.fn()}
        deps={deps()}
      />,
    );
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [
      file('a.mp3'),
      file('b.mp3'),
    ]);
    expect(
      await screen.findByText('a.mp3: нет связи с сервером — запись не прикрепилась'),
    ).toBeInTheDocument();
    expect(
      await screen.findByText('b.mp3: нет связи с сервером — запись не прикрепилась'),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Network Error|timeout of/)).not.toBeInTheDocument();
  });

  it('сервер ответил отказом — его текст из тела ответа', async () => {
    vi.spyOn(audioApi, 'recordingUploadURL').mockImplementation(() =>
      ok<RecordingUpload>({ key: 'k', url: 'u', content_type: 'audio/mpeg' }),
    );
    const refusal = new AxiosError('Request failed with status code 400', 'ERR_BAD_REQUEST');
    refusal.response = {
      status: 400,
      data: { message: 'длительность не сходится' },
    } as AxiosResponse;
    vi.spyOn(audioApi, 'registerRecording').mockImplementation(() => Promise.reject(refusal));
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={vi.fn()}
        deps={deps()}
      />,
    );
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [file('a.mp3')]);
    expect(await screen.findByText('a.mp3: длительность не сходится')).toBeInTheDocument();
  });

  // Тикет 07, п. 3: строки статуса искались по префиксу имени, и второй
  // «1.mp3» из другой папки стирал итог первого вместе с текстом ошибки.
  it('одинаковые имена в пачке — у каждого файла своя строка', async () => {
    vi.spyOn(audioApi, 'recordingUploadURL').mockImplementation(() =>
      ok<RecordingUpload>({ key: 'k', url: 'u', content_type: 'audio/mpeg' }),
    );
    vi.spyOn(audioApi, 'registerRecording').mockImplementation(() => ok(rec(1, 1)));
    const put = vi
      .fn()
      .mockRejectedValueOnce(new Error('заливка оборвалась'))
      .mockImplementation(
        (_u: string, _f: File, _t: string, onProgress: (l: number, t: number) => void) => {
          onProgress(1, 2);
          return Promise.resolve();
        },
      );
    const onChanged = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[]}
        onChanged={onChanged}
        deps={deps({ put })}
      />,
    );
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [
      file('1.mp3'),
      file('1.mp3'),
    ]);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    const lines = screen.getAllByRole('listitem').map((li) => li.textContent);
    expect(lines).toEqual(['1.mp3: заливка оборвалась', '1.mp3: прикреплена']);
  });

  it('перестановка', async () => {
    const update = vi.spyOn(audioApi, 'updateRecording').mockImplementation(() => ok(rec(2, 1)));
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1, 'Петров'), rec(2, 2)]}
        onChanged={vi.fn().mockResolvedValue(undefined)}
        deps={deps()}
      />,
    );
    await userEvent.click(screen.getAllByRole('button', { name: 'Выше' })[1]);
    expect(update).toHaveBeenCalledWith(2, { position: 1 });
    expect(screen.getAllByRole('button', { name: 'Выше' })[0]).toBeDisabled();
    expect(screen.getAllByRole('button', { name: 'Ниже' })[1]).toBeDisabled();
  });

  // Прежнее поле правилось молча по потере фокуса: не было видно ни что его
  // можно править, ни что правка сохранилась.
  it('чтец показан текстом; «Изменить» → поле, «Сохранить» пишет и закрывает поле', async () => {
    const update = vi.spyOn(audioApi, 'updateRecording').mockImplementation(() => ok(rec(1, 1)));
    const onChanged = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1, 'Петров'), rec(2, 2)]}
        onChanged={onChanged}
        deps={deps()}
      />,
    );
    expect(screen.getByText('Читает: Петров')).toBeInTheDocument();
    expect(screen.getByText('Чтец не указан')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /запись 1/ })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Изменить чтеца записи 1' }));
    const field = screen.getByRole('textbox', { name: 'Кто читает запись 1' });
    expect(field).toHaveValue('Петров');
    expect(field).toHaveFocus();
    await userEvent.clear(field);
    await userEvent.type(field, ' Сидоров ');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(update).toHaveBeenCalledWith(1, { reader: 'Сидоров' });
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(screen.queryByRole('textbox', { name: 'Кто читает запись 1' })).toBeNull();
  });

  it('Enter сохраняет, Esc и «Отмена» закрывают без записи', async () => {
    const update = vi.spyOn(audioApi, 'updateRecording').mockImplementation(() => ok(rec(1, 1)));
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1, 'Петров')]}
        onChanged={vi.fn().mockResolvedValue(undefined)}
        deps={deps()}
      />,
    );
    const open = () =>
      userEvent.click(screen.getByRole('button', { name: 'Изменить чтеца записи 1' }));

    await open();
    await userEvent.type(screen.getByRole('textbox', { name: 'Кто читает запись 1' }), 'а{Escape}');
    expect(screen.queryByRole('textbox', { name: 'Кто читает запись 1' })).toBeNull();
    await open();
    await userEvent.type(screen.getByRole('textbox', { name: 'Кто читает запись 1' }), 'б');
    await userEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(screen.queryByRole('textbox', { name: 'Кто читает запись 1' })).toBeNull();
    expect(update).not.toHaveBeenCalled();

    // Отменённый черновик не всплывает при следующем открытии.
    await open();
    const field = screen.getByRole('textbox', { name: 'Кто читает запись 1' });
    expect(field).toHaveValue('Петров');
    await userEvent.type(field, 'в{Enter}');
    expect(update).toHaveBeenCalledWith(1, { reader: 'Петровв' });
  });

  it('без изменений «Сохранить» запроса не шлёт; отказ сервера оставляет поле открытым', async () => {
    const update = vi
      .spyOn(audioApi, 'updateRecording')
      .mockImplementation(() => Promise.reject(new Error('нет')));
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1, 'Петров')]}
        onChanged={vi.fn().mockResolvedValue(undefined)}
        deps={deps()}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Изменить чтеца записи 1' }));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(update).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: 'Кто читает запись 1' })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Изменить чтеца записи 1' }));
    await userEvent.type(screen.getByRole('textbox', { name: 'Кто читает запись 1' }), 'а{Enter}');
    await waitFor(() => expect(update).toHaveBeenCalledWith(1, { reader: 'Петрова' }));
    // Правка не легла — набранное не пропадает.
    expect(screen.getByRole('textbox', { name: 'Кто читает запись 1' })).toHaveValue('Петрова');
  });

  // «Снять» читалось как «снять с публикации»; кнопка удаляет файл насовсем.
  it('«Удалить» спрашивает, называя запись, и удаляет только после согласия', async () => {
    const del = vi.spyOn(audioApi, 'deleteRecording').mockImplementation(() => ok(undefined));
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true);
    const onChanged = vi.fn().mockResolvedValue(undefined);
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1, 'Петров'), rec(2, 2)]}
        onChanged={onChanged}
        deps={deps()}
      />,
    );
    expect(screen.queryByRole('button', { name: /Снять/ })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Удалить запись 1' }));
    expect(confirm).toHaveBeenLastCalledWith(
      'Удалить запись 1 (читает Петров)? Файл удалится из хранилища безвозвратно.',
    );
    expect(del).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: 'Удалить запись 2' }));
    expect(confirm).toHaveBeenLastCalledWith(
      'Удалить запись 2? Файл удалится из хранилища безвозвратно.',
    );
    expect(del).toHaveBeenCalledWith(2);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  // Удалённую запись проигрыватель доигрывал бы до 410 на перемотке.
  it('удаление играющей записи закрывает проигрыватель, чужой — нет', async () => {
    vi.spyOn(audioApi, 'deleteRecording').mockImplementation(() => ok(undefined));
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const close = vi.fn();
    const playing = {
      key: 'rec:2',
      url: '',
      downloadUrl: '',
      title: '',
      subtitle: '',
      href: '',
      durationMs: 1,
    };
    usePlayer.setState({ queue: [playing], index: 0, status: 'playing', close });
    render(
      <RecordingManager
        workId={4}
        chapterId={223}
        recordings={[rec(1, 1), rec(2, 2)]}
        onChanged={vi.fn().mockResolvedValue(undefined)}
        deps={deps()}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Удалить запись 1' }));
    await waitFor(() => expect(audioApi.deleteRecording).toHaveBeenCalledWith(1));
    expect(close).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Удалить запись 2' }));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
  });
});
