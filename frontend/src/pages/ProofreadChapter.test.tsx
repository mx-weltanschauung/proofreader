import { render, screen, within, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { it, expect, vi, beforeEach } from 'vitest';
import { useAuth } from '../hooks/useAuth';
import type { User } from '../types';
import { proofreadAPI } from '../services/api';
import ProofreadChapter from './ProofreadChapter';
vi.mock('../services/api', () => ({
  API_BASE_URL: '',
  proofreadAPI: { get: vi.fn(), decide: vi.fn(), submit: vi.fn(), pair: vi.fn() },
}));
const findings = [
  {
    id: 1,
    start_rune: 2,
    end_rune: 3,
    old: 'а',
    new: 'б',
    reason: 'Первое место',
    source: 'model',
    decision: 'pending',
    replacement: '',
  },
  {
    id: 2,
    start_rune: 4,
    end_rune: 5,
    old: 'а',
    new: 'в',
    reason: 'Второе место',
    source: 'model',
    decision: 'pending',
    replacement: '',
  },
] as const;
beforeEach(() => {
  vi.resetAllMocks();
  useAuth.setState({ user: { id: 1, role: 'reader' } as User });
  vi.mocked(proofreadAPI.get).mockResolvedValue({
    id: 7,
    work_id: 1,
    chapter_id: 2,
    pages: [
      {
        page_id: 1,
        page_number: 126,
        base_markdown: '😀 а а',
        analysis_status: 'analyzed',
        findings: [...findings],
        scan_url: '/scan',
      },
      {
        page_id: 2,
        page_number: 127,
        base_markdown: '',
        analysis_status: '',
        findings: null,
        skip_reason: 'Скан недоступен',
      },
    ],
  });
});
async function open() {
  render(
    <MemoryRouter initialEntries={['/proofread/runs/7']}>
      <Routes>
        <Route path="/proofread/runs/:runId" element={<ProofreadChapter />} />
      </Routes>
    </MemoryRouter>,
  );
  await screen.findByRole('img', { name: 'Скан полосы 126' });
}
it('shows all findings, persists decisions and previews only ready pages', async () => {
  await open();
  expect(screen.getByText('Первое место')).toBeVisible();
  expect(screen.getByText('Второе место')).toBeVisible();
  const submit = screen.getByRole('button', { name: /отправить проверенные полосы/i });
  expect(submit).toBeDisabled();
  fireEvent.click(screen.getAllByRole('button', { name: 'Отклонить' })[0]);
  await waitFor(() => expect(proofreadAPI.decide).toHaveBeenCalledWith(7, 1, 'rejected', 'б'));
  fireEvent.click(screen.getAllByRole('button', { name: 'Принять' })[1]);
  await waitFor(() => expect(submit).toBeEnabled());
  expect(within(screen.getByLabelText('Предлагаемый Markdown')).getByText('😀 а в')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: /Полоса 127/ }));
  expect(screen.getByRole('status')).toHaveTextContent('Скан недоступен');
});
it.each(['rate_limited', 'stale'])('shows unsent result %s', async (status) => {
  await open();
  fireEvent.click(screen.getAllByRole('button', { name: 'Принять' })[0]);
  await waitFor(() => expect(screen.getAllByRole('button', { name: 'Принять' })[1]).toBeEnabled());
  fireEvent.click(screen.getAllByRole('button', { name: 'Принять' })[1]);
  vi.mocked(proofreadAPI.submit).mockResolvedValue({ pages: [{ page_id: 1, status }] });
  const submit = screen.getByRole('button', { name: /отправить проверенные полосы/i });
  await waitFor(() => expect(submit).toBeEnabled());
  fireEvent.click(submit);
  expect(
    (
      await screen.findAllByText(
        status === 'stale' ? /Исходный текст изменился/ : /Достигнут часовой лимит/,
      )
    ).length,
  ).toBeGreaterThan(0);
});

it('shows a changed base immediately on reopening the run', async () => {
  const run = await proofreadAPI.get(7);
  vi.mocked(proofreadAPI.get).mockResolvedValue({
    ...run,
    pages: [{ ...run.pages[0], stale: true }],
  });
  await open();
  expect(screen.getByRole('button', { name: 'Начать новую сверку главы' })).toBeVisible();
  expect(screen.getAllByRole('button', { name: 'Принять' })[0]).toBeDisabled();
});

it('shows the helper command with the issued code and run', async () => {
  await open();
  vi.mocked(proofreadAPI.pair).mockResolvedValue({ code: 'abcd1234' });
  fireEvent.click(screen.getByText('Подключить помощник для анализа'));
  fireEvent.click(screen.getByRole('button', { name: 'Получить одноразовый код' }));
  expect(
    await screen.findByText('proofread-assist pair abcd1234 --site-url ' + location.origin),
  ).toBeVisible();
  expect(screen.getByText('proofread-assist run 7 --model-url URL --model MODEL')).toBeVisible();
});
it('retains an edited replacement after switching pages and an HTTP 429', async () => {
  await open();
  fireEvent.click(screen.getAllByRole('button', { name: 'Изменить' })[1]);
  fireEvent.change(screen.getByRole('textbox', { name: 'Текст замены' }), {
    target: { value: '!' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Сохранить и принять' }));
  await waitFor(() => expect(proofreadAPI.decide).toHaveBeenCalledWith(7, 2, 'accepted', '!'));
  fireEvent.click(screen.getByRole('button', { name: /Полоса 127/ }));
  fireEvent.click(screen.getByRole('button', { name: /Полоса 126/ }));
  expect(screen.getByLabelText('Предлагаемый Markdown')).toHaveTextContent('😀 а !');
  const submit = screen.getByRole('button', { name: /отправить проверенные полосы/i });
  expect(submit).toBeDisabled();
  fireEvent.click(screen.getAllByRole('button', { name: 'Отклонить' })[0]);
  await waitFor(() => expect(submit).toBeEnabled());
  vi.mocked(proofreadAPI.submit).mockRejectedValue({
    isAxiosError: true,
    response: { status: 429 },
  });
  fireEvent.click(submit);
  expect(await screen.findByRole('alert')).toHaveTextContent(/часовой лимит/);
  expect(submit).toBeEnabled();
  expect(screen.getByLabelText('Предлагаемый Markdown')).toHaveTextContent('😀 а !');
});

it('reconciles a lost submission response with saved server receipts', async () => {
  const run = await proofreadAPI.get(7);
  const readyPage = {
    ...run.pages[0],
    findings: findings.map((f) => ({ ...f, decision: 'accepted' as const, replacement: f.new })),
  };
  vi.mocked(proofreadAPI.get).mockResolvedValue({ ...run, pages: [readyPage] });
  await open();
  vi.mocked(proofreadAPI.get).mockResolvedValue({
    ...run,
    pages: [{ ...readyPage, submitted_at: '2026-01-01' }],
  });
  vi.mocked(proofreadAPI.submit).mockRejectedValue({ isAxiosError: true, code: 'ERR_NETWORK' });
  fireEvent.click(screen.getByRole('button', { name: /отправить проверенные полосы/i }));
  expect((await screen.findAllByText('Предложение отправлено')).length).toBeGreaterThan(0);
  expect(screen.getByRole('button', { name: /отправить проверенные полосы/i })).toBeDisabled();
});
