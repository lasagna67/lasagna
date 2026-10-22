export type BusinessKind = "ip" | "ooo";

export const DEMO_PASSWORD = "DemoCabinet2026!";

export const scenarios = [
  { key: "good", label: "Хорошая кредитная история и достаточные финансы" },
  { key: "thin", label: "Небольшой запас денег" },
  { key: "overdue", label: "Текущая просрочка" },
  { key: "no-history", label: "Нет кредитной истории, но есть финансовые данные" },
  { key: "no-data", label: "Нет кредитной истории и финансовых данных" },
  { key: "terminated", label: "Деятельность прекращена" },
] as const;

export const innCatalog = [
  { kind: "ooo", company: "Волжский мебельный контур Демо", inn: "6315819669", status: "Действует", serial: "1D7598C07F22CF5C" },
  { kind: "ooo", company: "Маршрут кедра Демо", inn: "7709094259", status: "Реорганизация", serial: "72D433F7F3AD3930" },
  { kind: "ooo", company: "Невский модуль Демо", inn: "7813099373", status: "Реорганизация", serial: "1CF0513F5579DFB3" },
  { kind: "ooo", company: "Таёжный оттиск Демо", inn: "5406020303", status: "Деятельность прекращена", serial: "AC41A1AAA41CBCB0" },
  { kind: "ip", company: "Искролесов Никита Сергеевич", inn: "781663101333", status: "Действует", serial: "D083AEDC7E758BB7" },
  { kind: "ip", company: "Светоключев Артём Михайлович", inn: "231101208844", status: "Действует", serial: "3961D07FA432451B" },
  { kind: "ip", company: "Ветрокрылов Егор Вадимович", inn: "631652222490", status: "Действует", serial: "AA1DCF02BED44888" },
  { kind: "ip", company: "Таёжников Илья Андреевич", inn: "540742918750", status: "Деятельность прекращена", serial: "7FDCC374946C6F24" },
] as const;

export function scenarioEmail(kind: BusinessKind, scenarioKey: string): string {
  return `demo.${kind}.${scenarioKey}@lasagna.test`;
}

export function certSerialByInn(inn: string): string | undefined {
  return innCatalog.find((row) => row.inn === inn)?.serial;
}

const businessStatuses: Record<string, string> = {
  active: "Действует",
  reorganizing: "Реорганизация",
  terminated: "Деятельность прекращена",
};

export function businessStatusLabel(status: string): string {
  return businessStatuses[status] ?? status;
}

const positions: Record<string, string> = {
  owner: "Владелец",
  ceo: "Генеральный директор",
  cfo: "Финансовый директор",
  accountant: "Бухгалтер",
  finance_officer: "Финансовый сотрудник",
  authorized_employee: "Уполномоченный сотрудник",
};

export function positionLabel(position: string): string {
  return positions[position] ?? position;
}

const loanTypes: Record<string, string> = {
  business_loan: "Кредит бизнесу",
  equipment_loan: "Кредит на оборудование",
  consumer_loan: "Потребительский кредит",
};

export function loanTypeLabel(loanType: string): string {
  return loanTypes[loanType] ?? loanType;
}

const rub = new Intl.NumberFormat("ru-RU");

export function money(value: number): string {
  return `${rub.format(value)} ₽`;
}

const dateFmt = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "long",
  year: "numeric",
});

export function dateRu(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "" : dateFmt.format(date);
}

export function entityLabel(entityType: string): string {
  return entityType === "legal_entity" ? "ООО" : "ИП";
}

const loanStatuses: Record<string, string> = {
  active: "Активен",
  closed: "Закрыт",
  overdue: "Просрочка",
};

export function loanStatusLabel(status: string): string {
  return loanStatuses[status] ?? status;
}
