type Props = {
  message: string;
  onRetry?: () => void;
  className?: string;
};

export function ErrorBanner({ message, onRetry, className = "" }: Props) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className={`flex items-center justify-between gap-4 rounded-2xl bg-plate-tomato px-4 py-3 text-[14px] leading-5 ${className}`}
    >
      <span>{message}</span>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="shrink-0 font-medium text-brand hover:underline"
        >
          Повторить
        </button>
      )}
    </div>
  );
}
