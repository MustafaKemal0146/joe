"use client";

import {
  ArrowLeft,
  CheckCircle2,
  File,
  Folder,
  FolderOpen,
  HardDrive,
  LoaderCircle,
  Search,
  ShieldCheck,
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useState } from "react";
import { joeApi, readableError } from "../../lib/api";
import type {
  CorpusRecord,
  CorpusSearchResult,
  ImportBrowse,
  ImportStatus,
} from "../../lib/types";
import { DurumRozeti, HataKutusu } from "../ui";


export function DirectorySource({
  caseId,
  onPrepared,
}: {
  caseId: string;
  onPrepared: (text: string, title: string) => void;
}) {
  const [status, setStatus] = useState<ImportStatus | null>(null);
  const [browse, setBrowse] = useState<ImportBrowse | null>(null);
  const [corpora, setCorpora] = useState<CorpusRecord[]>([]);
  const [selectedCorpusId, setSelectedCorpusId] = useState("");
  const [query, setQuery] = useState("");
  const [result, setResult] = useState<CorpusSearchResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [indexing, setIndexing] = useState(false);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedCorpus = useMemo(
    () => corpora.find((item) => item.id === selectedCorpusId) ?? null,
    [corpora, selectedCorpusId],
  );

  async function refreshCorpora() {
    const items = await joeApi.corpora();
    setCorpora(items);
    setSelectedCorpusId((current) => current || items[0]?.id || "");
  }

  async function openDirectory(path: string) {
    setLoading(true);
    setError(null);
    try {
      setBrowse(await joeApi.browseImports(path));
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const [nextStatus, corpusItems] = await Promise.all([
          joeApi.importStatus(),
          joeApi.corpora(),
        ]);
        if (!active) return;
        setStatus(nextStatus);
        setCorpora(corpusItems);
        setSelectedCorpusId(corpusItems[0]?.id || "");
        if (nextStatus.available) setBrowse(await joeApi.browseImports("."));
      } catch (caught) {
        if (active) setError(readableError(caught));
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!selectedCorpus || !["queued", "running"].includes(selectedCorpus.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const updated = await joeApi.corpus(selectedCorpus.id);
        setCorpora((items) => items.map((item) => item.id === updated.id ? updated : item));
      } catch (caught) {
        setError(readableError(caught));
      }
    }, 1800);
    return () => window.clearInterval(timer);
  }, [selectedCorpus]);

  async function indexCurrent() {
    if (!browse) return;
    setIndexing(true);
    setError(null);
    try {
      const label = browse.path === "." ? "Bağlı dizin" : browse.path.split("/").at(-1) || "Dizin";
      const created = await joeApi.createCorpus({
        name: `${label} indeksi`,
        relative_path: browse.path,
        case_id: caseId || null,
      });
      await refreshCorpora();
      setSelectedCorpusId(created.id);
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setIndexing(false);
    }
  }

  async function searchCorpus(event: FormEvent) {
    event.preventDefault();
    if (!selectedCorpus || selectedCorpus.status !== "completed") return;
    setSearching(true);
    setError(null);
    try {
      const found = await joeApi.searchCorpus(selectedCorpus.id, { query, max_results: 30 });
      setResult(found);
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setSearching(false);
    }
  }

  return (
    <div className="dizin-kaynagi">
      <div className="dizin-guvenlik">
        <ShieldCheck size={16} />
        <span>Bağlı dizin salt-okunur taranır; ham dosyalar yüklenmez veya Joe alanına kopyalanmaz.</span>
      </div>

      {error ? <HataKutusu message={error} /> : null}
      {!loading && status && !status.available ? (
        <div className="dizin-bagli-degil">
          <HardDrive size={24} />
          <div><strong>İçe aktarma dizini bağlı değil</strong><span><code>JOE_IMPORT_PATH</code> değerini seçtiğin klasöre ayarlayıp Docker servislerini yeniden başlat.</span></div>
        </div>
      ) : null}

      {status?.available && browse ? (
        <div className="dizin-gezgini">
          <header>
            <div>
              <span>BAĞLI KÖK / {browse.path}</span>
              <strong><FolderOpen size={17} /> {browse.path === "." ? "Ana dizin" : browse.path}</strong>
            </div>
            <button type="button" className="birincil-buton" disabled={indexing} onClick={() => void indexCurrent()}>
              {indexing ? <LoaderCircle size={16} className="donen" /> : <HardDrive size={16} />}
              Bu dizini indeksle
            </button>
          </header>
          <div className="dizin-girdileri">
            {browse.parent_path ? (
              <button type="button" onClick={() => void openDirectory(browse.parent_path || ".")}>
                <ArrowLeft size={16} /><span><strong>Üst dizin</strong><small>{browse.parent_path}</small></span>
              </button>
            ) : null}
            {browse.entries.map((entry) => entry.kind === "dizin" ? (
              <button type="button" key={entry.relative_path} onClick={() => void openDirectory(entry.relative_path)}>
                <Folder size={17} /><span><strong>{entry.name}</strong><small>Dizin</small></span>
              </button>
            ) : (
              <div key={entry.relative_path}><File size={16} /><span><strong>{entry.name}</strong><small>{formatBytes(entry.size_bytes || 0)}</small></span></div>
            ))}
            {!browse.entries.length ? <p>Bu dizin boş.</p> : null}
          </div>
        </div>
      ) : loading ? <div className="dizin-yukleniyor"><LoaderCircle size={18} className="donen" /> Bağlı dizin okunuyor…</div> : null}

      {corpora.length ? (
        <section className="dizin-arama">
          <div className="dizin-indeks-secimi">
            <label><span>Hazır dizin indeksi</span><select value={selectedCorpusId} onChange={(event) => { setSelectedCorpusId(event.target.value); setResult(null); }}>
              {corpora.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.indexed_file_count} belge · {statusLabel(item.status)}</option>)}
            </select></label>
            {selectedCorpus ? <DurumRozeti durum={selectedCorpus.status} /> : null}
          </div>
          {selectedCorpus?.status === "failed" ? <HataKutusu message={selectedCorpus.error_message || "Dizin indekslenemedi."} /> : null}
          {selectedCorpus && ["queued", "running"].includes(selectedCorpus.status) ? (
            <div className="dizin-indeksleniyor"><LoaderCircle size={17} className="donen" /><span><strong>Dizin taranıyor</strong>{selectedCorpus.file_count} dosya görüldü, {selectedCorpus.indexed_file_count} belge indekslendi.</span></div>
          ) : null}
          <form onSubmit={searchCorpus} className="dizin-arama-formu">
            <Search size={18} />
            <input required minLength={2} value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Bu dizinde neyi araştırmak istiyorsun?" />
            <button className="birincil-buton" disabled={searching || selectedCorpus?.status !== "completed"}>{searching ? <LoaderCircle size={16} className="donen" /> : <Search size={16} />} Ara</button>
          </form>
          {result ? (
            <div className="dizin-sonuclari">
              <header><span><CheckCircle2 size={16} /> {result.hits.length} belge eşleşti</span><button type="button" disabled={!result.analysis_text} onClick={() => onPrepared(result.analysis_text, `${query.slice(0, 80)} analizi`)}>Sonuçları konseye aktar</button></header>
              {result.hits.slice(0, 12).map((hit) => <article key={hit.document_id}><strong>{hit.relative_path}</strong><small>{hit.extractor} · puan {hit.score}</small><p>{hit.excerpt}</p></article>)}
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}


function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}


function statusLabel(status: CorpusRecord["status"]): string {
  return {
    queued: "sırada",
    running: "çalışıyor",
    completed: "tamamlandı",
    failed: "başarısız",
  }[status];
}
