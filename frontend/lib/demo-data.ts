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
  { kind: "ooo", company: "ООО «Кедровый контур Демо»", inn: "7705005410", status: "Действует", serial: "0BFD0184037F4867" },
  { kind: "ooo", company: "ООО «Ладожский хлеб Демо»", inn: "7810969607", status: "Действует", serial: "62B6B06F3299A7A9" },
  { kind: "ooo", company: "ООО «Южный каркас Демо»", inn: "2310569351", status: "Действует", serial: "5C7C40739A13AA5B" },
  { kind: "ooo", company: "ООО «Листок данных Демо»", inn: "1655501828", status: "Действует", serial: "0D2979DB83EC40D5" },
  { kind: "ooo", company: "ООО «Искра макетов Демо»", inn: "6671986865", status: "Действует", serial: "E5CF051630872835" },
  { kind: "ooo", company: "ООО «Волжский ассортимент Демо»", inn: "5260947681", status: "Деятельность прекращена", serial: "9BE6A4ABE14CAE4D" },
  { kind: "ip", company: "ИП Кедровцев Матвей Олегович", inn: "770761836401", status: "Действует", serial: "630A9BBCC39C8358" },
  { kind: "ip", company: "ИП Ладогина Вероника Ильинична", inn: "781497718575", status: "Действует", serial: "8198C3C3BDFD70FF" },
  { kind: "ip", company: "ИП Листопадова Марина Денисовна", inn: "770815348700", status: "Действует", serial: "B3BF1FD8914CBBEF" },
  { kind: "ip", company: "ИП Яснолесов Даниил Романович", inn: "165979491871", status: "Действует", serial: "E2D03810DF340EB5" },
  { kind: "ip", company: "ИП Берестина Алиса Тимуровна", inn: "667036260874", status: "Действует", serial: "57A549E6CEBF5376" },
  { kind: "ip", company: "ИП Ручейников Глеб Павлович", inn: "526200583660", status: "Деятельность прекращена", serial: "3F792917D6FD2D33" },
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

export function dateRu(iso: string | null | undefined): string {
  if (!iso) return "—";
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "—" : dateFmt.format(date);
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

export function kopecks(value: number): string {
  const rubles = Math.floor(value / 100);
  const rest = value % 100;
  const formatted = rub.format(rubles);
  return rest === 0 ? `${formatted} ₽` : `${formatted},${String(rest).padStart(2, "0")} ₽`;
}

const appStatuses: Record<string, string> = {
  draft: "Черновик",
  submitted: "Отправлен",
  validation: "Проверка данных",
  enrichment: "Внешние проверки",
  scoring: "Скоринг",
  manual_review: "Рассмотрение сотрудника",
  info_requested: "Запрошены уточнения",
  approved: "Одобрено",
  counter_offer: "Контрпредложение",
  signing: "Подписание",
  issued: "Кредит выдан",
  rejected: "Отказ",
  cancelled: "Отменён",
  expired: "Предложение истекло",
};

export function appStatusLabel(status: string): string {
  return appStatuses[status] ?? status;
}

export const pipelineStages = [
  "submitted",
  "validation",
  "enrichment",
  "scoring",
  "manual_review",
] as const;

export const purposes: Record<string, string> = {
  working_capital: "Оборотные средства",
  cash_gap: "Кассовый разрыв",
  equipment: "Оборудование",
  business_start: "Старт бизнеса",
  business_expansion: "Расширение",
  other: "Другое",
};

export function purposeLabel(purpose: string | null): string {
  if (!purpose) return "—";
  return purposes[purpose] ?? purpose;
}

const collateralTypes: Record<string, string> = {
  real_estate: "Недвижимость",
  equipment: "Оборудование",
  vehicle: "Транспорт",
};

export function collateralTypeLabel(type: string): string {
  return collateralTypes[type] ?? type;
}

const riskGrades: Record<string, string> = {
  low: "Низкий",
  medium: "Средний",
  high: "Высокий",
  critical: "Критический",
};

export function riskGradeLabel(grade: string): string {
  return riskGrades[grade] ?? grade;
}
