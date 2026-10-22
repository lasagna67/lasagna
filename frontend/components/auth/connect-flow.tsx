"use client";

import { useState } from "react";
import { ApiError, api, type Challenge } from "@/lib/api";
import { certSerialByInn } from "@/lib/demo-data";
import { InnModal } from "./inn-modal";
import { SignModal } from "./sign-modal";

type Props = {
  token: string;
  onClose: () => void;
  onConnected: () => void;
};

type Busy = "lookup" | "sign" | "connect" | null;

function message(e: unknown): string {
  return e instanceof ApiError ? e.message : "Неизвестная ошибка";
}

export function ConnectFlow({ token, onClose, onConnected }: Props) {
  const [phase, setPhase] = useState<"inn" | "sign">("inn");
  const [inn, setInn] = useState<string | null>(null);
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [ownerName, setOwnerName] = useState("");
  const [pin, setPin] = useState("12345678");
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState("");

  async function loadOwner(serial: string) {
    try {
      const { certificates } = await api.certificates();
      setOwnerName(certificates.find((c) => c.serial === serial)?.subject.full_name ?? "");
    } catch {
      setOwnerName("");
    }
  }

  async function continueWithInn() {
    if (!inn) return;
    setBusy("lookup");
    setError("");
    try {
      await api.lookup(inn, token);
      const next = await api.createChallenge(inn, token);
      setChallenge(next);
      setPhase("sign");
      void loadOwner(certSerialByInn(inn) ?? "");
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(null);
    }
  }

  async function sign() {
    if (!inn || !challenge) return;
    setBusy("sign");
    setError("");
    let signed: { signature: string };
    try {
      signed = await api.sign(certSerialByInn(inn) ?? "", pin, challenge.data);
    } catch (e) {
      setError(e instanceof ApiError && e.status === 403 ? "Неверный PIN-код" : message(e));
      setBusy(null);
      return;
    }

    setBusy("connect");
    try {
      await api.connect(challenge.challenge_id, signed.signature, token);
      onConnected();
      return;
    } catch (e) {
      // Заявление одноразовое: готовим новое, чтобы разрешить повторную попытку
      const next = await api.createChallenge(inn, token).catch(() => null);
      if (next) setChallenge(next);
      setError(
        (e instanceof ApiError && e.status === 410
          ? "Заявление истекло или уже использовано."
          : `${message(e)}.`) + " Подготовили новое заявление — подпишите заново.",
      );
      setBusy(null);
    }
  }

  if (phase === "sign" && challenge) {
    return (
      <SignModal
        challenge={challenge}
        ownerName={ownerName}
        pin={pin}
        error={error}
        busy={busy === "lookup" ? null : busy}
        onPin={setPin}
        onSign={sign}
        onClose={onClose}
      />
    );
  }

  return (
    <InnModal
      inn={inn}
      error={error}
      busy={busy === "lookup"}
      onSelect={(value) => {
        setInn(value);
        setError("");
      }}
      onContinue={continueWithInn}
      onClose={onClose}
    />
  );
}
