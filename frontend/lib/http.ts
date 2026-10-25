export type FieldError = { field: string; code: string; message: string };

export class ApiError extends Error {
  status: number;
  code: string;
  fieldErrors: FieldError[];
  currentRevision?: number;

  constructor(
    status: number,
    message: string,
    code = "",
    fieldErrors: FieldError[] = [],
    currentRevision?: number,
  ) {
    super(message);
    this.status = status;
    this.code = code;
    this.fieldErrors = fieldErrors;
    this.currentRevision = currentRevision;
  }
}

export function isRevisionConflict(e: unknown): e is ApiError {
  return e instanceof ApiError && e.code === "REVISION_CONFLICT";
}

export function newIdemKey(): string {
  return crypto.randomUUID();
}

type Options = {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
  form?: FormData;
  token?: string;
  idem?: string;
  query?: Record<string, string | number | undefined | null>;
  base?: string;
};

function url(base: string, path: string, query?: Options["query"]): string {
  const qs = query
    ? Object.entries(query)
        .filter(([, v]) => v !== undefined && v !== null && v !== "")
        .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`)
        .join("&")
    : "";
  return `${base}${path}${qs ? `?${qs}` : ""}`;
}

async function errorFrom(res: Response): Promise<ApiError> {
  const data = (await res.json().catch(() => null)) as {
    message?: string;
    error?: {
      code?: string;
      message?: string;
      field_errors?: FieldError[];
      current_revision?: number;
    };
  } | null;
  const body = data?.error;
  return new ApiError(
    res.status,
    body?.message ?? data?.message ?? "Сервис недоступен, попробуйте позже",
    body?.code ?? "",
    body?.field_errors ?? [],
    body?.current_revision,
  );
}

async function send(path: string, opts: Options, wantBlob: boolean): Promise<unknown> {
  const base = opts.base ?? "/api/v0";
  let res: Response;
  try {
    res = await fetch(url(base, path, opts.query), {
      method: opts.method ?? "GET",
      headers: {
        ...(opts.body ? { "Content-Type": "application/json" } : {}),
        ...(opts.token ? { Authorization: `Bearer ${opts.token}` } : {}),
        ...(opts.idem ? { "Idempotency-Key": opts.idem } : {}),
      },
      body: opts.form ?? (opts.body ? JSON.stringify(opts.body) : undefined),
    });
  } catch {
    throw new ApiError(0, "Сервис недоступен, попробуйте позже");
  }
  if (!res.ok) throw await errorFrom(res);
  if (wantBlob) return res.blob();
  if (res.status === 204) return undefined;
  return res.json().catch(() => undefined);
}

export function request<T>(path: string, opts: Options = {}): Promise<T> {
  return send(path, opts, false) as Promise<T>;
}

export function requestBlob(path: string, opts: Options = {}): Promise<Blob> {
  return send(path, opts, true) as Promise<Blob>;
}
