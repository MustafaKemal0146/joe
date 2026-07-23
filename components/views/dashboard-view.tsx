"use client";

import {
  ArrowRight,
  Binoculars,
  BrainCircuit,
  BriefcaseBusiness,
  Clock3,
  FolderKanban,
  PlugZap,
  Radio,
} from "lucide-react";
import type { CaseRecord, DashboardSummary, Gorunum } from "../../lib/types";
import { DurumRozeti, HataKutusu, Yukleniyor } from "../ui";


export function DashboardView({
  summary,
  cases,
  loading,
  apiHealthy,
  onNavigate,
}: {
  summary: DashboardSummary | null;
  cases: CaseRecord[];
  loading: boolean;
  apiHealthy: boolean;
  onNavigate: (view: Gorunum) => void;
}) {
  return (
    <div className="vaka-masasi">
      <header className="vaka-masasi-baslik">
        <div>
          <span className="eyebrow">VAKA ÇALIŞMA ALANI</span>
          <h1>Vaka Masası</h1>
          <p>Devam eden araştırmalar ve analiz oturumları.</p>
        </div>
        <div className="vaka-masasi-aksiyonlar">
          <button className="ikincil-buton" onClick={() => onNavigate("analiz")}>
            <BrainCircuit size={17} /> Analiz başlat
          </button>
          <button className="birincil-buton" onClick={() => onNavigate("osint")}>
            <Binoculars size={17} /> Yeni araştırma
          </button>
        </div>
      </header>

      {!apiHealthy && !loading ? (
        <HataKutusu message="Joe API servisine ulaşılamıyor. Docker servislerinin durumunu denetle." />
      ) : null}

      <section className="masa-metrikleri" aria-label="Çalışma alanı özeti">
        {loading ? <Yukleniyor label="Vaka kayıtları okunuyor" /> : (
          <>
            <article><FolderKanban size={18} /><span>Vaka</span><strong>{summary?.case_count ?? 0}</strong></article>
            <article><Binoculars size={18} /><span>OSINT çalışması</span><strong>{summary?.osint_run_count ?? 0}</strong></article>
            <article><BrainCircuit size={18} /><span>Konsey oturumu</span><strong>{summary?.analysis_count ?? 0}</strong></article>
            <article><Radio size={18} /><span>Aktif iş</span><strong>{summary?.active_jobs ?? 0}</strong></article>
          </>
        )}
      </section>

      <div className="vaka-masasi-grid">
        <section className="panel vaka-listesi-paneli">
          <div className="panel-baslik">
            <div><span className="eyebrow">SON GÜNCELLENENLER</span><h2>Vakalar</h2></div>
            <button className="metin-buton" onClick={() => onNavigate("vakalar")}>Tümünü aç <ArrowRight size={15} /></button>
          </div>
          {cases.length ? (
            <div className="masa-vaka-listesi">
              {cases.slice(0, 8).map((item) => (
                <article key={item.id}>
                  <span className="vaka-simgesi"><BriefcaseBusiness size={17} /></span>
                  <div>
                    <strong>{item.name}</strong>
                    <small>{item.subject_label || "Özne etiketi yok"}</small>
                  </div>
                  <time dateTime={item.updated_at}><Clock3 size={13} /> {new Date(item.updated_at).toLocaleDateString("tr-TR")}</time>
                  <DurumRozeti durum={item.status} />
                </article>
              ))}
            </div>
          ) : (
            <div className="masa-bos">
              <BriefcaseBusiness size={25} />
              <strong>Henüz vaka yok</strong>
              <span>İlk OSINT araştırması başlatıldığında vaka otomatik açılır.</span>
              <button onClick={() => onNavigate("osint")}>Araştırma başlat <ArrowRight size={15} /></button>
            </div>
          )}
        </section>

        <aside className="masa-yan-panel">
          <section className="panel masa-modul-karti">
            <Binoculars size={21} />
            <div><span>Keşfet</span><strong>OSINT</strong><small>Tüm uyumlu kaynaklar otomatik çalışır.</small></div>
            <button onClick={() => onNavigate("osint")} aria-label="OSINT alanını aç"><ArrowRight size={17} /></button>
          </section>
          <section className="panel masa-modul-karti">
            <BrainCircuit size={21} />
            <div><span>İncele</span><strong>Kuramsal Konsey</strong><small>Tek tam protokol: görüş, itiraz, revizyon, sentez.</small></div>
            <button onClick={() => onNavigate("analiz")} aria-label="Analiz alanını aç"><ArrowRight size={17} /></button>
          </section>
          <section className="panel masa-modul-karti">
            <PlugZap size={21} />
            <div><span>Bağlantılar</span><strong>{summary?.provider_connection_count ?? 0} AI sağlayıcısı</strong><small>Yalnız senin eklediğin gerçek bağlantılar.</small></div>
            <button onClick={() => onNavigate("saglayicilar")} aria-label="Sağlayıcıları aç"><ArrowRight size={17} /></button>
          </section>
        </aside>
      </div>
    </div>
  );
}
