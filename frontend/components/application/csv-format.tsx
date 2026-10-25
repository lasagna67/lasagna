const columns: [string, string][] = [
  ["inn", "ИНН компании — должен совпадать с подключённой компанией"],
  ["month", "месяц в формате YYYY-MM, без повторов"],
  ["income", "поступления, копейки"],
  ["expenses", "выплаты, копейки"],
  ["taxes", "налоги, копейки"],
  ["owner_withdrawals", "выплаты владельцу, копейки"],
];

export function CsvFormat() {
  return (
    <div className="rounded-2xl border border-black/8 bg-plate-pearl/60 px-4 py-3 text-[14px]">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium">Формат CSV</span>
        <a href="/financials_example.csv" download className="text-brand hover:underline">
          Скачать пример (financials_example.csv)
        </a>
      </div>
      <p className="mt-2 font-mono text-[13px]">inn,month,income,expenses,taxes,owner_withdrawals</p>
      <ul className="mt-2 space-y-1">
        {columns.map(([col, text]) => (
          <li key={col} className="flex flex-wrap gap-x-2">
            <code className="font-mono text-[13px]">{col}</code>
            <span className="text-ink-soft">{text}</span>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-ink-soft">
        Заголовок и порядок колонок строго те же; суммы — целые неотрицательные копейки.
      </p>
    </div>
  );
}
