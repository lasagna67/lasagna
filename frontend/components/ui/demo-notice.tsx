export function DemoNotice({ className = "" }: { className?: string }) {
  return (
    <p className={`rounded-2xl bg-plate-pearl px-4 py-3 text-[14px] leading-5 text-ink-soft ${className}`}>
      <span className="font-medium text-ink">Демонстрационный режим.</span> Для удобства проверки
      подготовлены тестовые аккаунты, компании и данные. Электронная подпись имитируется.
    </p>
  );
}
