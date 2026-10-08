import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import { useRef } from 'react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Link } from 'react-router-dom';
import { HintsProvider } from './HintsProvider';
import { useHints } from './hintsContext';
import { HINTS_KEY, readHints } from '../hints/hintsStorage';
import { HINTS, HINT_APPEAR_MS, type HintId } from '../hints/registry';
import { Header } from '../components/Header';
import { FeatureHint } from '../components/FeatureHint';
import { ReadingPreferencesProvider } from './ReadingPreferencesContext';
import { useAuth } from '../hooks/useAuth';
import { installHintHarness, removeHintHarness, sayVisible, advance } from '../test/hintHarness';

/** Токен пульта: у настоящих выносок его выдаёт useId, тут хватает постоянного. */
const PROBE_TOKEN = 'пульт';

/** Пульт: регистрирует две подсказки и показывает, кого выбрал координатор. */
function Probe({ ids }: { ids: HintId[] }) {
  const hints = useHints();
  if (!hints) return <p>без провайдера</p>;
  return (
    <div>
      <p data-testid="active">{hints.active?.id ?? 'нет'}</p>
      <button onClick={() => hints.reset()}>сброс</button>
      {ids.map((id) => (
        <span key={id}>
          <button onClick={() => hints.register(id, PROBE_TOKEN)}>рег {id}</button>
          <button onClick={() => hints.setVisible(id, PROBE_TOKEN, true)}>виден {id}</button>
          <button onClick={() => hints.setVisible(id, PROBE_TOKEN, false)}>скрыт {id}</button>
          <button onClick={() => hints.markOpened(id, PROBE_TOKEN)}>показал {id}</button>
          <button onClick={() => hints.learn(id)}>усвоил {id}</button>
          <button onClick={() => hints.close(id, true)}>закрыл со счётом {id}</button>
          <button onClick={() => hints.close(id, false)}>закрыл без счёта {id}</button>
        </span>
      ))}
    </div>
  );
}

function setup(ids: HintId[]) {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <HintsProvider>
        <Probe ids={ids} />
      </HintsProvider>
    </MemoryRouter>,
  );
  return { user, active: () => screen.getByTestId('active').textContent };
}

/**
 * Два экземпляра ОДНОГО органа: кнопка «Aa» смонтирована и в шапке сайта, и в
 * панели чтения. Регистрируются они по одному id, но разными токенами.
 */
function TwinProbe() {
  const hints = useHints();
  if (!hints) return <p>без провайдера</p>;
  const id: HintId = 'reading-settings';
  return (
    <div>
      <p data-testid="active">
        {hints.active ? `${hints.active.id}:${hints.active.token}` : 'нет'}
      </p>
      {['шапка', 'панель'].map((token) => (
        <span key={token}>
          <button onClick={() => hints.register(id, token)}>рег {token}</button>
          <button onClick={() => hints.setVisible(id, token, true)}>виден {token}</button>
          <button onClick={() => hints.setVisible(id, token, false)}>скрыт {token}</button>
          <button onClick={() => hints.unregister(id, token)}>снял {token}</button>
        </span>
      ))}
    </div>
  );
}

function setupTwins() {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <HintsProvider>
        <TwinProbe />
      </HintsProvider>
    </MemoryRouter>,
  );
  return { user, active: () => screen.getByTestId('active').textContent };
}

describe('орган, смонтированный дважды', () => {
  // Кнопка «Aa» есть и в шапке сайта, и в панели чтения. На экране чтения
  // шапка скрыта через display:none, её якорь не виден — право показаться
  // обязано достаться той копии, у которой якорь на экране, а не «первой
  // попавшейся с этим id».
  it('право показаться достаётся видимому экземпляру, а не идентификатору', async () => {
    const { user, active } = setupTwins();
    await user.click(screen.getByText('рег шапка'));
    await user.click(screen.getByText('рег панель'));
    await user.click(screen.getByText('виден панель'));
    expect(active()).toBe('reading-settings:панель');
  });

  it('невидимый экземпляр права не получает', async () => {
    const { user, active } = setupTwins();
    await user.click(screen.getByText('рег шапка'));
    await user.click(screen.getByText('рег панель'));
    expect(active()).toBe('нет');
  });

  // Прежде видимость хранилась одним слотом на идентификатор, и размонтирование
  // одной копии стирало видимость другой: панель чтения уходит — выноска у
  // шапки гаснет ни с того ни с сего.
  it('снятие одного экземпляра не гасит другой', async () => {
    const { user, active } = setupTwins();
    await user.click(screen.getByText('рег шапка'));
    await user.click(screen.getByText('рег панель'));
    await user.click(screen.getByText('виден панель'));
    await user.click(screen.getByText('снял шапка'));
    expect(active()).toBe('reading-settings:панель');
  });
});

beforeEach(() => localStorage.clear());

describe('координатор подсказок', () => {
  it('без видимого якоря никого не выбирает', async () => {
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    expect(active()).toBe('нет');
  });

  it('выбирает подсказку с наибольшим приоритетом среди видимых', async () => {
    // outline-depth (80) против download (60).
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('рег outline-depth'));
    // Оба якоря становятся видимыми в одном цикле рендера — реальная
    // ситуация: несколько органов на экране готовы одновременно. Через
    // отдельные `await user.click` так не смоделировать: каждый клик — свой
    // проход эффектов, и координатор успел бы замкнуться на download раньше,
    // чем видимым станет outline-depth (это уже проверяет следующий тест).
    // Два fireEvent в одном act() дают один проход эффектов на оба сразу.
    act(() => {
      fireEvent.click(screen.getByText('виден download'));
      fireEvent.click(screen.getByText('виден outline-depth'));
    });
    expect(active()).toBe('outline-depth');
  });

  // Приоритет обязан решать очерёдность, а не гонка загрузки. Якорь «Aa»
  // живёт в шапке сайта и виден с первого кадра, а собственные органы экрана
  // приезжают после ответа API — если бы замок был неотзывным, каждый экран
  // объяснял бы «Aa» и никогда не доходил до своего.
  it('право переходит к более приоритетной, пока пузырёк не показан', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('download');
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    expect(active()).toBe('outline-depth');
  });

  // Обратное: менее приоритетная не отбирает право обратно.
  it('право не переходит к менее приоритетной', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('outline-depth');
  });

  // Выбранная выноска не должна меняться под читателем: появился якорь
  // важнее — ждёт следующего захода.
  it('не переключается на более приоритетную, пока показывает первую', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('download');
    // Пузырёк на экране: с этого мгновения замок неприкосновенен.
    await user.click(screen.getByText('показал download'));
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    expect(active()).toBe('download');
  });

  it('после засчитанного показа на этом экране больше никого не показывает', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('показал download'));
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    // download появился первым и замкнулся раньше outline-depth (см. «не
    // переключается...» выше) — закрываем именно замкнутую подсказку.
    expect(active()).toBe('download');
    await user.click(screen.getByText('закрыл со счётом download'));
    expect(active()).toBe('нет');
  });

  // Охрана close: она обязана сверяться с тем, кто реально замкнут, а не
  // просто принимать любой переданный id. close описывает исход показа
  // («сколько провисела, пока гасла») — утверждение, бессмысленное для
  // подсказки, которую сейчас не показывают. Без охраны вызов с чужим id
  // (подсказка в очереди, а не показанная) начислил бы ей показ и погасил бы
  // активную.
  it('закрытие со счётом чужой подсказки не начисляет ей показ и не гасит активную', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('показал download'));
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    expect(active()).toBe('download'); // download замкнут, outline-depth в очереди

    await user.click(screen.getByText('закрыл со счётом outline-depth'));
    expect(readHints()['outline-depth']).toBeUndefined();
    expect(active()).toBe('download');
  });

  // В отличие от close, у learn охрана не на само действие, а только на
  // право экрана: усвоение — факт про подсказку («читатель нашёл орган сам»),
  // истинный независимо от того, показывали её вообще. Он должен закрепиться
  // всегда. А вот право экрана и активная выноска — общий, разделяемый на
  // экран ресурс: их усвоение чужой (незамкнутой) подсказки трогать не
  // должно, иначе клик по органу, до которого координатор ещё не дошёл, мог
  // бы погасить действительно показанную выноску или списать чужое право.
  it('усвоение чужой подсказки помечает её усвоенной, но не гасит активную', async () => {
    const { user, active } = setup(['download', 'outline-depth']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('показал download'));
    await user.click(screen.getByText('рег outline-depth'));
    await user.click(screen.getByText('виден outline-depth'));
    expect(active()).toBe('download'); // download замкнут, outline-depth в очереди

    await user.click(screen.getByText('усвоил outline-depth'));
    expect(readHints()['outline-depth']?.done).toBe(true);
    expect(active()).toBe('download');
  });

  // Якорь мелькнул и исчез — показа не было, право экрана не потрачено.
  it('незасчитанный показ не тратит право экрана', async () => {
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('закрыл без счёта download'));
    expect(active()).toBe('download');
  });

  it('засчитанный показ пишется в хранилище', async () => {
    const { user } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('закрыл со счётом download'));
    expect(readHints().download).toEqual({ seen: 1, done: false });
  });

  it('усвоенная подсказка не выбирается снова', async () => {
    localStorage.setItem(HINTS_KEY, JSON.stringify({ download: { seen: 0, done: true } }));
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('нет');
  });

  it('после трёх показов подсказка усвоена', async () => {
    localStorage.setItem(HINTS_KEY, JSON.stringify({ download: { seen: 2, done: false } }));
    const { user } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('закрыл со счётом download'));
    expect(readHints().download).toEqual({ seen: 3, done: false });
    // seen === SEEN_LIMIT — координатор её больше не выберет.
    expect(screen.getByTestId('active').textContent).toBe('нет');
  });

  it('нажатие органа помечает подсказку усвоенной навсегда', async () => {
    const { user } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('усвоил download'));
    expect(readHints().download?.done).toBe(true);
  });

  it('снятие с регистрации возвращает координатора к пустому выбору', async () => {
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('скрыт download'));
    expect(active()).toBe('нет');
  });

  it('вне провайдера хук отдаёт null, а не бросает', () => {
    render(<Probe ids={['download']} />);
    expect(screen.getByText('без провайдера')).toBeInTheDocument();
  });

  // reset() возвращает координатора к чистому выбору, не трогая localStorage:
  // очисткой ключа занимается страница справки в отдельной задаче, и её тест
  // проверяет именно очистку ключа, а не поведение координатора.
  it('reset() снова предлагает усвоенную подсказку, не трогая localStorage', async () => {
    localStorage.setItem(HINTS_KEY, JSON.stringify({ download: { seen: 3, done: true } }));
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('нет');

    await user.click(screen.getByText('сброс'));
    expect(active()).toBe('download');
    expect(localStorage.getItem(HINTS_KEY)).toBe(
      JSON.stringify({ download: { seen: 3, done: true } }),
    );
  });

  // Два FeatureHint с одним и тем же id (например, второй некорректно
  // сделанный «опт-ин» рядом с ReadingSettings) не должны занимать второй
  // слот в anchors и гоняться за него unregister'ом — вместо тихого рецидива
  // выбран громкий console.warn в деве (см. комментарий у register в
  // HintsProvider.tsx). Клик по «рег» дважды подряд для одного и того же id —
  // тот же случай, что и два реальных монтажа: register() вызывается второй
  // раз поверх уже занятого слота.
  // Прежде повторная регистрация одного id считалась дефектом и печатала
  // предупреждение: слот видимости был один на идентификатор, и копии органа
  // дрались за него. Теперь это законный случай (кнопка «Aa» смонтирована в
  // шапке сайта и в панели чтения одновременно) — см. describe «орган,
  // смонтированный дважды». Здесь остаётся то, что от него требовалось и
  // тогда: повторная регистрация тем же токеном ничего не ломает и не
  // плодит второй экземпляр.
  it('повторная регистрация тем же токеном идемпотентна', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { user, active } = setup(['download']);
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    expect(active()).toBe('download');
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  // Право экрана привязано к экрану (маршруту), а не к жизни компонента:
  // WorkRead меняет /works/N/read/K на другую страницу тем же тома через
  // history.replaceState — react-router это не замечает, поэтому сброс по
  // pathname здесь остаётся непроверенным без прямой навигации через Link.
  it('смена маршрута снимает потраченное право экрана', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/a']}>
        <HintsProvider>
          <Probe ids={['download']} />
          <Link to="/b">дальше</Link>
        </HintsProvider>
      </MemoryRouter>,
    );
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('закрыл со счётом download'));
    expect(screen.getByTestId('active').textContent).toBe('нет');

    await user.click(screen.getByText('дальше'));
    // Проба не размонтировалась (Link — не Route), anchors не тронуты: якорь
    // остаётся видимым, и одного сброса spent/locked достаточно для нового
    // показа на новом экране.
    expect(screen.getByTestId('active').textContent).toBe('download');
  });

  // Контраст к предыдущему тесту: без навигации то же самое действие не
  // должно ничего перевооружать — сброс завязан именно на смену pathname, а
  // не на что-то более широкое вроде переоценки anchors.
  it('без смены маршрута потраченное право экрана не возвращается', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/a']}>
        <HintsProvider>
          <Probe ids={['download']} />
        </HintsProvider>
      </MemoryRouter>,
    );
    await user.click(screen.getByText('рег download'));
    await user.click(screen.getByText('виден download'));
    await user.click(screen.getByText('закрыл со счётом download'));
    expect(screen.getByTestId('active').textContent).toBe('нет');

    // Тот же маршрут, якорь мигнул — право экрана потрачено раньше и должно
    // оставаться потраченным.
    await user.click(screen.getByText('скрыт download'));
    await user.click(screen.getByText('виден download'));
    expect(screen.getByTestId('active').textContent).toBe('нет');
  });
});

// Regression: FeatureHint «reading-settings» рисовался ReadingSettings
// безусловно, а Header (и потому этот компонент) смонтирован на каждом
// экране, включая карточку тома — там «Aa» реально видна, а не CSS-скрыта,
// как на экране чтения. При приоритете 90 против 80 у outline-depth это
// значило, что глубина содержания никогда не получала показа, стоило шапке
// оказаться в кадре одновременно с ней. Фикс — withHint как опт-ин
// (по умолчанию false, включает только ReadingStreamBar) — проверяется здесь
// на дереве, максимально похожем на настоящую карточку тома: Header без
// правки плюс якорь outline-depth, оба наблюдателя IntersectionObserver
// получают сигнал «виден» одним и тем же тиком.
describe('приоритет на дереве, похожем на карточку тома', () => {
  function OutlineDepthProbe() {
    const ref = useRef<HTMLButtonElement>(null);
    return (
      <>
        <button ref={ref} type="button">
          глубина
        </button>
        <FeatureHint id="outline-depth" anchorRef={ref} />
      </>
    );
  }

  beforeEach(() => {
    localStorage.clear();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    installHintHarness();
  });
  afterEach(removeHintHarness);

  it('шапка сайта не отбирает показ у outline-depth', () => {
    render(
      <MemoryRouter>
        <ReadingPreferencesProvider>
          <HintsProvider>
            <Header />
            <OutlineDepthProbe />
          </HintsProvider>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );
    // «Aa» в шапке и «глубина» карточки тома входят во вьюпорт одним тиком —
    // ровно ситуация карточки тома, где шапка сайта всегда на экране.
    sayVisible(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveTextContent(HINTS['outline-depth'].text);
  });
});
