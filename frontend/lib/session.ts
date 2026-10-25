type Session = {
  accessToken: string;
  email?: string;
  role?: string;
  expiresAt?: string;
};

const KEY = "lasagna.session";

export function getSession(): Session | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const session = JSON.parse(raw) as Session;
    if (session.expiresAt && Date.parse(session.expiresAt) <= Date.now()) {
      clearSession();
      return null;
    }
    return session;
  } catch {
    return null;
  }
}

export function setSession(session: Session) {
  localStorage.setItem(KEY, JSON.stringify(session));
}

export function clearSession() {
  localStorage.removeItem(KEY);
}

export function homeForRole(role?: string): string {
  return role === "underwriter" || role === "admin" ? "/underwriter" : "/cabinet";
}
