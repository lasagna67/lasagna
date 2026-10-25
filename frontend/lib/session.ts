type Session = {
  accessToken: string;
  email?: string;
  role?: string;
  expiresAt?: string;
};

const KEY = "lasagna.session";

function parse(raw: string | null): Session | null {
  if (!raw) return null;
  const session = JSON.parse(raw) as Session;
  if (session.expiresAt && Date.parse(session.expiresAt) <= Date.now()) return null;
  return session;
}

// Сессия живёт в sessionStorage: окна одного браузера (клиент и банкир в
// разных вкладках) не перезаписывают токены друг друга. localStorage хранит
// последний вход — новая вкладка открывается уже залогиненной.
export function getSession(): Session | null {
  if (typeof window === "undefined") return null;
  try {
    const own = parse(sessionStorage.getItem(KEY));
    if (own) return own;
    const shared = parse(localStorage.getItem(KEY));
    if (shared) {
      sessionStorage.setItem(KEY, JSON.stringify(shared));
      return shared;
    }
    clearSession();
    return null;
  } catch {
    return null;
  }
}

export function setSession(session: Session) {
  sessionStorage.setItem(KEY, JSON.stringify(session));
  localStorage.setItem(KEY, JSON.stringify(session));
}

export function clearSession() {
  sessionStorage.removeItem(KEY);
  localStorage.removeItem(KEY);
}

export function homeForRole(role?: string): string {
  return role === "underwriter" || role === "admin" ? "/underwriter" : "/cabinet";
}
