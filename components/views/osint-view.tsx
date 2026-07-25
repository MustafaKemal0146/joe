"use client";

import {
  ArrowUpRight,
  Binoculars,
  CheckCircle2,
  Clock3,
  Fingerprint,
  FolderKanban,
  Globe2,
  LoaderCircle,
  Search,
  ShieldAlert,
  UserRoundSearch,
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import { joeApi, readableError } from "../../lib/api";
import type { CaseRecord, OsintRun } from "../../lib/types";
import { DurumRozeti, HataKutusu, SayfaBasligi } from "../ui";


export function OsintView({ cases, onChanged }: { cases: CaseRecord[]; onChanged?: () => void | Promise<void> }) {
  const [queryType, setQueryType] = useState<"username" | "full_name">("username");
  const [query, setQuery] = useState("");
  const [caseId, setCaseId] = useState("");
  const [run, setRun] = useState<OsintRun | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!run || !["queued", "running"].includes(run.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const updated = await joeApi.osintRun(run.id);
        setRun(updated);
      } catch (caught) {
        setError(readableError(caught));
      }
    }, 1800);
    return () => window.clearInterval(timer);
  }, [run]);

  const findings = useMemo(
    () => run?.result?.findings ?? Object.values(run?.result?.connector_results ?? {}).flatMap((result) => result.findings ?? []),
    [run],
  );

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    setRun(null);
    try {
      const nextRun = await joeApi.createOsintRun({
        query,
        query_type: queryType,
        case_id: caseId || null,
      });
      setRun(nextRun);
      await onChanged?.();
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div>
      <SayfaBasligi
        eyebrow="BAĞIMSIZ MODÜL / KEŞFET"
        title="OSINT Keşif Masası"
        description="Kullanıcı adı veya tam ad için araştırma yolları üret, gerçek bağlayıcıları çalıştır ve her eşleşmeyi aday olarak doğrula."
        actions={<span className="ayrim-etiketi"><ShieldAlert size={16} /> Psikoanalizden ayrı</span>}
      />

      <section className="osint-arama-paneli">
        <div className="arama-turleri" role="tablist" aria-label="Arama türü">
          <button className={queryType === "username" ? "aktif" : ""} onClick={() => setQueryType("username")} role="tab" aria-selected={queryType === "username"}>
            <Fingerprint size={17} /> Kullanıcı adı
          </button>
          <button className={queryType === "full_name" ? "aktif" : ""} onClick={() => setQueryType("full_name")} role="tab" aria-selected={queryType === "full_name"}>
            <UserRoundSearch size={17} /> Tam ad
          </button>
        </div>
        <form onSubmit={submit} className="arama-formu">
          <div className="ana-arama-girdisi">
            <Search size={21} aria-hidden="true" />
            <input
              required
              minLength={2}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={queryType === "username" ? "Kullanıcı adını yaz…" : "Ad ve soyadı yaz…"}
              aria-label={queryType === "username" ? "Kullanıcı adı" : "Tam ad"}
            />
            <button className="birincil-buton" disabled={loading || !caseId}>
              {loading ? <LoaderCircle size={17} className="donen" /> : <Binoculars size={17} />}
              Keşfi başlat
            </button>
          </div>
          <div className="arama-alt-ayarlar">
            <label>
              <FolderKanban size={15} />
              <select value={caseId} onChange={(event) => setCaseId(event.target.value)} aria-label="Vaka seçimi" required>
                <option value="">— Vaka seçin veya oluşturun —</option>
                {cases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select>
            </label>
            <span><Globe2 size={15} /> Yalnız açık ve elle doğrulanabilir kaynaklar</span>
            {queryType === "username" ? <span><CheckCircle2 size={15} /> Tüm uyumlu bağlayıcılar otomatik</span> : <span><CheckCircle2 size={15} /> Açık profil dizinleri otomatik</span>}
          </div>
          {!caseId ? (
            <div className="text-sm text-amber-600 mt-2 flex items-center gap-1">
              <ShieldAlert size={14} /> Araştırmayı başlatmak için bir vaka seçmelisiniz.
            </div>
          ) : null}
        </form>
      </section>

      {error ? <HataKutusu message={error} /> : null}

      {run ? (
        <section className="calisma-durum-cubugu">
          <div className="calisma-durum-sol">
            {run.status === "running" || run.status === "queued" ? <LoaderCircle size={18} className="donen" /> : <CheckCircle2 size={18} />}
            <div><strong>{run.query}</strong><span>{run.query_type === "username" ? "Kullanıcı adı keşfi" : "Tam ad keşfi"}</span></div>
          </div>
          <div className="calisma-durum-sag">
            <DurumRozeti durum={run.status} />
            <a href={`/osint/calisma/${run.id}`} className="text-xs text-blue-600 hover:underline"><code>{run.id.slice(0, 8)}</code></a>
          </div>
        </section>
      ) : null}

      {run?.result?.errors?.length ? (
        <div className="hata-listesi">
          {run.result.errors.map((item) => <HataKutusu key={`${item.connector}-${item.code}`} message={`${connectorLabel(item.connector)}: ${item.message}`} />)}
        </div>
      ) : null}

      {run ? (
        <section className="panel bulgu-paneli">
          <div className="panel-baslik">
            <div><span className="eyebrow">TEKİLLEŞTİRİLMİŞ SONUÇLAR</span><h2>Bulunan bağlantılar</h2></div>
            <span className="sayac">{findings.length}</span>
          </div>
          {run && ["queued", "running"].includes(run.status) ? (
            <div className="tarama-bekleme"><LoaderCircle size={24} className="donen" /><strong>Uygun kaynakların tamamı taranıyor</strong><span>Sherlock, Maigret, WhatsMyName ve açık profil dizinleri aynı çalışmada yürütülüyor.</span></div>
          ) : findings.length ? (
            <div className="bulgu-listesi">
              {findings.map((finding, index) => (
                <article key={`${finding.profile_url}-${index}`}>
                  <div className="bulgu-site"><span>{finding.site?.slice(0, 1) || "?"}</span><div><strong>{finding.site}</strong><small>@{finding.username} · {finding.sources.map(connectorLabel).join(" + ")}</small></div></div>
                  <div className="bulgu-dogrulama"><span className="aday-nokta" />{finding.identity_status}</div>
                  <a className="bulgu-linki" href={finding.profile_url} target="_blank" rel="noreferrer" aria-label={`${finding.site} profilini aç`}><span>{finding.profile_url.replace(/^https?:\/\//, "")}</span><ArrowUpRight size={16} /></a>
                </article>
              ))}
            </div>
          ) : (
            <div className="tarama-bekleme"><Clock3 size={24} /><strong>Henüz bağlayıcı bulgusu yok</strong><span>Bu alan yalnızca gerçek tarama sonucu geldiğinde dolar.</span></div>
          )}
          <div className="bulgu-uyarisi"><ShieldAlert size={16} /> Aynı kullanıcı adı, aynı kişi demek değildir. Her profil ayrıca doğrulanmalıdır.</div>
        </section>
      ) : (
        <section className="osint-baslangic">
          <div className="osint-baslangic-ikon"><Binoculars size={28} /></div>
          <h2>Bir kimlik ipucuyla başla</h2>
          <p>Joe önce araştırma adaylarını toplar. Bir kaydı doğrulanmış olguya dönüştürme kararı analiste aittir.</p>
          <div><span>01</span> Sorguyu belirle <i /><span>02</span> Kaynakları çalıştır <i /><span>03</span> Eşleşmeleri doğrula</div>
        </section>
      )}
    </div>
  );
}


function connectorLabel(value: string): string {
  const [connector, source] = value.split("/", 2);
  const labels: Record<string, string> = {
    sherlock: "Sherlock",
    maigret: "Maigret",
    whatsmyname: "WhatsMyName",
    public_profiles: "Açık profil dizinleri",
  };
  return source ? `${labels[connector] ?? connector} / ${source}` : labels[connector] ?? connector;
}
