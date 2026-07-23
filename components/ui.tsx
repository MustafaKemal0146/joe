"use client";

import type { ReactNode } from "react";
import { AlertTriangle, LoaderCircle } from "lucide-react";

export function SayfaBasligi({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <header className="sayfa-basligi">
      <div>
        <span className="eyebrow">{eyebrow}</span>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {actions ? <div className="sayfa-aksiyonlari">{actions}</div> : null}
    </header>
  );
}

export function DurumRozeti({ durum }: { durum: string }) {
  const map: Record<string, string> = {
    queued: "Sırada",
    running: "Çalışıyor",
    completed: "Tamamlandı",
    failed: "Başarısız",
    cancelled: "İptal edildi",
    active: "Etkin",
  };
  return <span className={`durum-rozeti durum-${durum}`}>{map[durum] ?? durum}</span>;
}

export function HataKutusu({ message }: { message: string }) {
  return (
    <div className="hata-kutusu" role="alert">
      <AlertTriangle size={17} aria-hidden="true" />
      <span>{message}</span>
    </div>
  );
}

export function Yukleniyor({ label = "Yükleniyor" }: { label?: string }) {
  return (
    <div className="yukleniyor" role="status">
      <LoaderCircle size={18} className="donen" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

export function BosDurum({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="bos-durum">
      <div className="bos-durum-ikonu">{icon}</div>
      <h3>{title}</h3>
      <p>{description}</p>
      {action}
    </div>
  );
}

export function KisaTarih({ value }: { value: string }) {
  return (
    <time dateTime={value}>
      {new Intl.DateTimeFormat("tr-TR", {
        day: "2-digit",
        month: "short",
        hour: "2-digit",
        minute: "2-digit",
      }).format(new Date(value))}
    </time>
  );
}
