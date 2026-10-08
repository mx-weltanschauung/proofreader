import toast from 'react-hot-toast';

interface AskAiButtonProps {
  /** Готовый запрос; зовётся в момент нажатия. */
  prompt: () => string;
}

/**
 * «Спросить нейросеть» — кладёт в буфер готовый запрос со ссылкой на текст
 * (главы или понятия). Одна кнопка без названий чужих сервисов: работает с любым чатом,
 * в том числе без адреса с предзаполненным запросом. Как это устроено —
 * /help#ask-ai.
 */
export function AskAiButton({ prompt }: AskAiButtonProps) {
  const onClick = async () => {
    try {
      // Между нажатием и записью в буфер нет ни одного await: иначе браузер
      // сочтёт жест пользователя исчерпанным и откажет (тот же довод, что у
      // CiteButton).
      await navigator.clipboard.writeText(prompt());
      toast.success(
        <span>
          Скопировано — вставьте в чат нейросети. <a href="/help#ask-ai">Как это работает</a>
        </span>,
      );
    } catch {
      toast.error('Не удалось скопировать');
    }
  };
  return (
    <button type="button" className="btn btn-secondary" onClick={() => void onClick()}>
      Спросить нейросеть
    </button>
  );
}
