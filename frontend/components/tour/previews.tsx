// Статичные мини-формы для туториала: показывают, как выглядят ответ на
// запрос банка и форма подписи, когда живого блока на странице ещё нет.

export function AnswerPreview() {
  return (
    <div className="mt-3 rounded-2xl border border-black/8 bg-plate-pearl/70 p-4 text-[14px] text-ink">
      <div className="flex items-center gap-3">
        <span className="font-medium">Банк запросил уточнения</span>
        <span className="rounded-full bg-plate-chardonnay px-2 py-0.5 text-[12px]">
          открытые поля: сумма
        </span>
      </div>
      <p className="mt-2 text-ink-soft">Приложите счёт поставщика оборудования</p>
      <div className="mt-3 rounded-xl border border-black/8 bg-white px-3 py-2 text-ink-soft">
        Ваш ответ банку…
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        <span className="rounded-full bg-brand px-3 py-1 text-[13px] text-white">Ответить</span>
        <span className="rounded-full bg-black/6 px-3 py-1 text-[13px]">
          Отправить заявку повторно
        </span>
      </div>
    </div>
  );
}

export function SignPreview() {
  return (
    <div className="mt-3 rounded-2xl border border-black/8 bg-plate-pearl/70 p-4 text-[14px] text-ink">
      <span className="font-medium">Подписание договора</span>
      <div className="mt-2 flex h-16 items-center justify-center rounded-xl border border-dashed border-black/15 bg-white text-[13px] text-ink-soft">
        PDF договора
      </div>
      <p className="mt-3 text-[13px]">☑ Согласен с условиями: сумма, ставка, срок</p>
      <div className="mt-2 flex items-center gap-2 text-[13px] text-ink-soft">
        <span>PIN-код сертификата</span>
        <span className="rounded-lg border border-black/10 bg-white px-3 py-1">••••••••</span>
      </div>
      <span className="mt-3 inline-block rounded-full bg-brand px-4 py-1.5 text-[13px] text-white">
        Подписать
      </span>
    </div>
  );
}
