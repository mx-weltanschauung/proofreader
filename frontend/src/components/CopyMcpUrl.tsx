import toast from 'react-hot-toast';

interface CopyMcpUrlProps {
  url: string;
  /** Что сказать, когда скопировано; по умолчанию — про адрес. */
  done?: string;
}

/**
 * Кнопка «Скопировать» у адреса MCP-сервера читальни в /help#mcp (адрес
 * вставляют в настройки коннектора Claude или приложения ChatGPT) и у адреса
 * каталога OPDS в /help#opds (его вставляют в читалку). В /help#offline
 * копирует контрольную сумму архива — отсюда подпись done.
 */
export function CopyMcpUrl({ url, done = 'Адрес скопирован' }: CopyMcpUrlProps) {
  const onClick = async () => {
    try {
      // Между нажатием и записью в буфер нет ни одного await (довод CiteButton).
      await navigator.clipboard.writeText(url);
      toast.success(done);
    } catch {
      toast.error('Не удалось скопировать');
    }
  };
  return (
    <button type="button" className="btn btn-secondary" onClick={() => void onClick()}>
      Скопировать
    </button>
  );
}
