/**
 * Поле «Раздел на главной» — общее у формы собрания и у правки организации.
 * Свободный ввод с подсказкой уже заведённых имён: новый раздел заводится
 * вписанным именем, без правки кода.
 */
export function ShelfGroupField({
  id,
  value,
  onChange,
  options,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  options: string[];
}) {
  return (
    <div className="form-group">
      <label htmlFor={id}>Раздел на главной</label>
      <input
        id={id}
        list="shelf-group-options"
        maxLength={80}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <datalist id="shelf-group-options">
        {options.map((name) => (
          <option key={name} value={name} />
        ))}
      </datalist>
      <p className="form-help-text">
        Пусто — «Собрания сочинений». Новое имя заводит новый раздел.
      </p>
    </div>
  );
}
