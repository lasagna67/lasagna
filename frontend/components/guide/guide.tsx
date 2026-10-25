import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { scenarios } from "@/lib/demo-data";

const products = [
  {
    name: "Экспресс-кредит",
    note: "Текущие нужды: закупки, расчёты, прочее",
    amount: "100 000 – 5 000 000 ₽",
    term: "3–36 мес.",
    collateral: "Без залога",
    rate: "22,9%",
  },
  {
    name: "Оборотные средства",
    note: "Оборот бизнеса и кассовые разрывы",
    amount: "500 000 – 50 000 000 ₽",
    term: "6–36 мес.",
    collateral: "Не обязателен",
    rate: "19,9%",
  },
  {
    name: "Оборудование и транспорт",
    note: "Покупка техники и транспорта",
    amount: "1 000 000 – 100 000 000 ₽",
    term: "12–84 мес.",
    collateral: "Обязателен",
    rate: "18,5%",
  },
  {
    name: "Овердрафт",
    note: "Лимит на кассовые разрывы, транш до 60 дней",
    amount: "100 000 – 10 000 000 ₽",
    term: "12 мес.",
    collateral: "Без залога",
    rate: "21,5%",
  },
  {
    name: "«Старт»",
    note: "Запуск или расширение бизнеса",
    amount: "100 000 – 3 000 000 ₽",
    term: "12–36 мес.",
    collateral: "Обязателен",
    rate: "24,0%",
  },
];

const steps = [
  {
    title: "Параметры",
    text: "Укажите цель, сумму, срок и обеспечение. Подходящие продукты показываем сразу — выберите один из предложенных.",
  },
  {
    title: "Залог",
    text: "Только если выбрали «С залогом»: предмет, описание и заявленная стоимость.",
  },
  {
    title: "Проект и денежный план",
    text: "Только для продукта «Старт»: описание проекта, статьи расходов и помесячный план поступлений и выплат.",
  },
  {
    title: "Финансы",
    text: "Нажмите «Получить данные из источника» — обычно этого хватает. Если источник не ответил, загрузите CSV с оборотами. Вариант «Истории нет» доступен только для «Старта».",
  },
  {
    title: "Отправка",
    text: "Проверьте сводку и отправьте заявку. Если финансовые данные так и не получены, CSV можно загрузить прямо на этом шаге.",
  },
];

const after = [
  {
    plate: "bg-plate-sonata",
    title: "Проверка",
    text: "Заявка проходит проверку данных, внешние проверки и скоринг автоматически — обычно за минуту. Дальше её берёт сотрудник банка; страница обновляется сама.",
  },
  {
    plate: "bg-plate-chardonnay",
    title: "Запрошены уточнения",
    text: "Банк оставляет сообщение и открывает отдельные поля для правки. Ответьте, поправьте открытые поля и нажмите «Отправить заявку повторно».",
  },
  {
    plate: "bg-plate-menthol",
    title: "Одобрено или контрпредложение",
    text: "В кабинете появятся сумма, ставка, срок, платёж и PDF договора. Откройте договор, прочитайте условия и подпишите — в демо PIN сертификата 12345678. После подписи доступны подписанный PDF и JSON-артефакт подписи.",
  },
  {
    plate: "bg-plate-tomato",
    title: "Отказ",
    text: "Заявка закрыта — детали можно уточнить в банке.",
  },
];

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-t border-black/8 py-10 md:py-14">
      <h2 className="text-promo-m">{title}</h2>
      <div className="mt-6 space-y-4 text-[16px] leading-6">{children}</div>
    </section>
  );
}

export function Guide() {
  return (
    <Container className="pb-16">
      <h1 className="mt-10 max-w-[18ch] text-promo-xl md:mt-14">Как подать заявку</h1>
      <p className="mt-5 max-w-[62ch] text-[18px] leading-7 text-ink-soft">
        Короткая инструкция для кабинета: как войти, что за типы кредитов, как заполнить заявку и
        что происходит после отправки.
      </p>

      <Section title="Вход и аккаунты">
        <p>
          На экране входа выберите тип аккаунта: <strong>ООО</strong>, <strong>ИП</strong> или{" "}
          <strong>Банкир</strong> — последний открывает кабинет сотрудника, если вы проверяете
          вторую сторону.
        </p>
        <p>
          Дальше выберите сценарий — это готовый аккаунт с определённой ситуацией компании:
        </p>
        <ul className="grid gap-2 sm:grid-cols-2">
          {scenarios.map((s) => (
            <li key={s.key} className="rounded-2xl bg-plate-pearl px-4 py-3 text-[15px] leading-5">
              {s.label}
            </li>
          ))}
        </ul>
        <p>
          Вход в сценарий — в один клик, пароль <code className="font-mono">DemoCabinet2026!</code>{" "}
          подставляется сам; почта вида{" "}
          <code className="font-mono">demo.ooo.good@lasagna.test</code>. Свой аккаунт можно создать
          через «Зарегистрировать нового пользователя»: email, пароль и подключение компании по ИНН
          из реестра (например, 7705005410 — ООО, 770761836401 — ИП).
        </p>
      </Section>

      <Section title="Типы кредитов">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-[15px]">
            <thead className="text-[14px] text-ink-soft">
              <tr className="border-b border-black/8">
                <th className="py-2 pr-4 font-normal">Продукт</th>
                <th className="py-2 pr-4 font-normal">Сумма</th>
                <th className="py-2 pr-4 font-normal">Срок</th>
                <th className="py-2 pr-4 font-normal">Залог</th>
                <th className="py-2 font-normal">Ставка</th>
              </tr>
            </thead>
            <tbody>
              {products.map((p) => (
                <tr key={p.name} className="border-b border-black/6 align-top">
                  <td className="py-3 pr-4">
                    <span className="font-medium">{p.name}</span>
                    <span className="block text-[13px] leading-4 text-ink-soft">{p.note}</span>
                  </td>
                  <td className="py-3 pr-4 whitespace-nowrap">{p.amount}</td>
                  <td className="py-3 pr-4 whitespace-nowrap">{p.term}</td>
                  <td className="py-3 pr-4">{p.collateral}</td>
                  <td className="py-3 whitespace-nowrap">{p.rate}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="text-ink-soft">
          Продукт подбирается под цель и сумму заявки — на шаге «Параметры» мы предложим подходящие
          варианты.
        </p>
      </Section>

      <Section title="Заявка по шагам">
        <ol className="space-y-5">
          {steps.map((step, i) => (
            <li key={step.title} className="flex gap-4">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-brand text-[14px] font-medium text-white">
                {i + 1}
              </span>
              <span>
                <span className="font-medium">{step.title}</span>
                <span className="block text-ink-soft">{step.text}</span>
              </span>
            </li>
          ))}
        </ol>
      </Section>

      <Section title="После отправки">
        <div className="space-y-4">
          {after.map((item) => (
            <div key={item.title} className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <span className={`shrink-0 rounded-full px-3 py-1 text-[14px] ${item.plate}`}>
                {item.title}
              </span>
              <span className="min-w-[260px] flex-1 text-ink-soft">{item.text}</span>
            </div>
          ))}
        </div>
        <p className="text-ink-soft">
          Для CSV действуют те же правила парсера: колонки{" "}
          <code className="font-mono">inn,month,income,expenses,taxes,owner_withdrawals</code>,
          суммы целыми копейками, только завершённые месяцы не раньше регистрации компании.
        </p>
        <div className="flex flex-wrap items-center gap-4 pt-2">
          <Button href="/login">Войти в кабинет</Button>
          <a href="/financials_example.csv" download className="text-[16px] text-brand hover:underline">
            Скачать пример CSV
          </a>
        </div>
      </Section>
    </Container>
  );
}
