import React from 'react';
import MDEditor from '@uiw/react-md-editor';
import '@uiw/react-md-editor/markdown-editor.css';
import '@uiw/react-markdown-preview/markdown.css';
import 'katex/dist/katex.min.css';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import rehypeSanitize from 'rehype-sanitize';
import type { PluggableList } from 'unified';
import { markdownPreviewSanitizeSchema } from './markdownSanitizeSchema';
import { rehypeAllowlistStyles } from './rehypeAllowlistStyles';
import { useReadingPrefs } from '../contexts/readingPrefsContext';
import { resolveTheme } from '../contexts/readingPrefs';
import InnerImageZoom from 'react-inner-image-zoom';
import 'react-inner-image-zoom/lib/styles.min.css';

interface MarkdownEditorProps {
  initialContent: string;
  onChange: (value: string) => void;
  onSubmit?: () => void;
  imageUrl?: string;
  /**
   * Opt-in: sanitize the live preview's rendered HTML.
   *
   * `@uiw/react-markdown-preview` unconditionally runs `rehype-raw` ahead of
   * any caller-supplied rehype plugins, so raw HTML embedded in the markdown
   * source (e.g. `<img src=x onerror=...>`) becomes live, script-executing
   * DOM by default — fine for the proofreader-editing screens, where the
   * text is trusted, but not for previewing markdown submitted by anonymous
   * readers (the moderation queue, the public suggestion form). Set this to
   * `true` wherever the content being previewed did not originate from an
   * authenticated editor. Default is unchanged (`false`) so existing,
   * trusted call sites keep today's behavior exactly.
   */
  sanitizeUntrustedContent?: boolean;
}

type EditorViewMode = 'editor-preview' | 'editor-scan';

export const MarkdownEditor: React.FC<MarkdownEditorProps> = ({
  initialContent,
  onChange,
  onSubmit,
  imageUrl,
  sanitizeUntrustedContent = false,
}) => {
  const [value, setValue] = React.useState(initialContent);
  const rehypePlugins: PluggableList = sanitizeUntrustedContent
    ? [rehypeKatex, [rehypeSanitize, markdownPreviewSanitizeSchema], rehypeAllowlistStyles]
    : [rehypeKatex];
  const [viewMode, setViewMode] = React.useState<EditorViewMode>(
    imageUrl ? 'editor-scan' : 'editor-preview',
  );
  const editorRef = React.useRef<HTMLDivElement>(null);
  const { prefs } = useReadingPrefs();

  const [prevInitialContent, setPrevInitialContent] = React.useState(initialContent);
  if (prevInitialContent !== initialContent) {
    setPrevInitialContent(initialContent);
    setValue(initialContent);
  }

  const handleChange = (newValue?: string) => {
    const val = newValue || '';
    setValue(val);
    onChange(val);
  };

  React.useEffect(() => {
    if (!editorRef.current || !onSubmit) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        onSubmit();
      }
    };

    const editorElement = editorRef.current;
    editorElement.addEventListener('keydown', handleKeyDown, { capture: false });

    return () => {
      editorElement.removeEventListener('keydown', handleKeyDown);
    };
  }, [onSubmit]);

  const resolvedTheme = resolveTheme(
    prefs.theme,
    window.matchMedia('(prefers-color-scheme: dark)').matches,
  );
  const colorMode =
    resolvedTheme === 'dark' || resolvedTheme === 'oled' || resolvedTheme === 'gray-dim'
      ? 'dark'
      : 'light';

  React.useEffect(() => {
    if (!editorRef.current) return;

    const applyStyles = () => {
      const textareas = editorRef.current?.querySelectorAll('textarea');
      textareas?.forEach((textarea) => {
        const root = document.documentElement;
        const computedStyle = getComputedStyle(root);
        const textColor = computedStyle.getPropertyValue('--color-text-primary').trim();

        if (textColor) {
          textarea.style.setProperty('color', textColor, 'important');
          textarea.style.setProperty('-webkit-text-fill-color', textColor, 'important');
        }
      });
    };

    const timeoutId = setTimeout(applyStyles, 0);
    const observer = new MutationObserver(applyStyles);

    if (editorRef.current) {
      observer.observe(editorRef.current, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['data-color-mode'],
      });
    }

    return () => {
      clearTimeout(timeoutId);
      observer.disconnect();
    };
  }, [prefs.theme, value]);

  return (
    <div data-color-mode={colorMode} ref={editorRef}>
      <div
        style={{
          background: 'var(--color-bg-tertiary)',
          padding: '8px 12px',
          borderBottom: '1px solid var(--color-border)',
          fontSize: '12px',
          color: 'var(--color-text-secondary)',
          marginBottom: '8px',
          borderRadius: '4px 4px 0 0',
        }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <strong style={{ color: 'var(--color-text-primary)' }}>Редактор разметки</strong>
            <span style={{ marginLeft: '16px' }}>
              Ctrl+Enter — сохранить • Ctrl+B — жирный • Ctrl+I — курсив • Ctrl+K — ссылка
            </span>
            <div style={{ marginTop: '4px', fontSize: '11px', opacity: 0.8 }}>
              Формулы (KaTeX): инлайн $x^2$ или блоком $$\int_0^1 x^2 dx$$
            </div>
          </div>
          {imageUrl && (
            <div style={{ display: 'flex', gap: '8px' }}>
              <button
                onClick={() => setViewMode('editor-preview')}
                aria-pressed={viewMode === 'editor-preview'}
                style={{
                  padding: '4px 12px',
                  fontSize: '12px',
                  background:
                    viewMode === 'editor-preview' ? 'var(--color-button-primary)' : 'transparent',
                  color:
                    viewMode === 'editor-preview'
                      ? 'var(--color-info-on)'
                      : 'var(--color-text-primary)',
                  border: '1px solid var(--color-border)',
                  borderRadius: '4px',
                  cursor: 'pointer',
                }}
              >
                Редактор + предпросмотр
              </button>
              <button
                onClick={() => setViewMode('editor-scan')}
                aria-pressed={viewMode === 'editor-scan'}
                style={{
                  padding: '4px 12px',
                  fontSize: '12px',
                  background:
                    viewMode === 'editor-scan' ? 'var(--color-button-primary)' : 'transparent',
                  color:
                    viewMode === 'editor-scan'
                      ? 'var(--color-info-on)'
                      : 'var(--color-text-primary)',
                  border: '1px solid var(--color-border)',
                  borderRadius: '4px',
                  cursor: 'pointer',
                }}
              >
                Редактор + скан
              </button>
            </div>
          )}
        </div>
      </div>

      {viewMode === 'editor-preview' ? (
        <MDEditor
          value={value}
          onChange={handleChange}
          height={600}
          preview="live"
          hideToolbar={false}
          enableScroll={true}
          visibleDragbar={true}
          previewOptions={{
            remarkPlugins: [remarkMath],
            rehypePlugins,
          }}
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '1fr 1fr',
            gap: '20px',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <MDEditor
              value={value}
              onChange={handleChange}
              height={600}
              preview="edit"
              hideToolbar={false}
              enableScroll={true}
              visibleDragbar={false}
              previewOptions={{
                remarkPlugins: [remarkMath],
                rehypePlugins,
              }}
            />
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', position: 'sticky', top: 0 }}>
            <div
              style={{
                height: '600px',
                border: '1px solid var(--color-border)',
                borderRadius: '4px',
                padding: '12px',
                background: 'var(--color-bg-primary)',
                overflow: 'auto',
              }}
            >
              {imageUrl && (
                <InnerImageZoom
                  src={imageUrl}
                  zoomSrc={imageUrl}
                  zoomType="hover"
                  zoomScale={1.5}
                />
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
