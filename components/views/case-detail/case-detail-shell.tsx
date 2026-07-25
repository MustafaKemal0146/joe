"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { joeApi } from "@/lib/api";
import type { CaseRecord, CaseWorkspace } from "@/lib/types";
import { AlertTriangle, CheckCircle2, FileText, FolderOpen, LoaderCircle, Search } from "lucide-react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";

export function CaseDetailShell({ caseId }: { caseId: string }) {
  const [caseRecord, setCaseRecord] = useState<CaseRecord | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    joeApi.cases().then((cases) => {
      const found = cases.find((c) => c.id === caseId);
      setCaseRecord(found || null);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, [caseId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <LoaderCircle className="animate-spin text-zinc-500" size={32} />
      </div>
    );
  }

  if (!caseRecord) {
    return <div className="p-8 text-zinc-500">Vaka bulunamadı.</div>;
  }

  return (
    <div className="p-6 max-w-6xl mx-auto">
      {/* Vaka başlığı */}
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
        <p className="text-xs font-semibold text-zinc-400 uppercase tracking-wide">VAKA DOSYASI</p>
        <h1 className="text-2xl font-bold text-zinc-900 mt-1">{caseRecord.name}</h1>
        {caseRecord.subject_label && (
          <p className="text-sm text-zinc-500 mt-1">Özne: {caseRecord.subject_label}</p>
        )}
        <div className="flex items-center gap-3 mt-2 text-sm text-zinc-500">
          <span className="px-2 py-0.5 rounded bg-zinc-100 text-xs">
            {caseRecord.status}
          </span>
          <span>Oluşturma: {new Date(caseRecord.created_at).toLocaleDateString("tr-TR")}</span>
        </div>
        </div>
        <Link href="/" className="ikincil-buton shrink-0"><ArrowLeft size={16} /> Vaka masasına dön</Link>
      </div>

      {/* Sekmeler */}
      <CaseDetailTabs caseId={caseId} caseName={caseRecord.name} />
    </div>
  );
}

function CaseDetailTabs({ caseId, caseName }: { caseId: string; caseName: string }) {
  const [activeTab, setActiveTab] = useState("overview");
  const [workspace, setWorkspace] = useState<CaseWorkspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    joeApi.caseWorkspace(caseId)
      .then(setWorkspace)
      .catch((caught) => setError(caught instanceof Error ? caught.message : "Vaka çalışma alanı okunamadı."))
      .finally(() => setLoading(false));
  }, [caseId]);

  const tabs = [
    { id: "overview", label: "Genel Bakış" },
    { id: "osint", label: "OSINT" },
    { id: "analyses", label: "Analizler" },
    { id: "evidence", label: "Kanıtlar" },
  ];

  return (
    <div>
      <div className="flex gap-1 border-b mb-4">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.id
                ? "border-zinc-900 text-zinc-900"
                : "border-transparent text-zinc-500 hover:text-zinc-700"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <div className="min-h-[300px]">
        {loading ? <div className="flex items-center gap-2 text-sm text-zinc-500 py-8"><LoaderCircle className="animate-spin" size={17} /> Vaka kayıtları yükleniyor…</div> : null}
        {error ? <div className="flex items-center gap-2 text-sm text-red-600 py-8"><AlertTriangle size={17} /> {error}</div> : null}
        {!loading && !error && workspace && activeTab === "overview" ? <OverviewTab caseId={caseId} caseName={caseName} workspace={workspace} /> : null}
        {!loading && !error && workspace && activeTab === "osint" ? <OsintTab workspace={workspace} /> : null}
        {!loading && !error && workspace && activeTab === "analyses" ? <AnalysesTab workspace={workspace} /> : null}
        {!loading && !error && workspace && activeTab === "evidence" ? <EvidenceTab workspace={workspace} /> : null}
      </div>
    </div>
  );
}

function OverviewTab({ caseId, caseName, workspace }: { caseId: string; caseName: string; workspace: CaseWorkspace }) {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <div className="p-4 bg-white border rounded">
          <p className="text-xs text-zinc-400 uppercase">Vaka Adı</p>
          <p className="font-medium">{caseName}</p>
        </div>
        <div className="p-4 bg-white border rounded"><p className="text-xs text-zinc-400 uppercase">OSINT çalışması</p><p className="font-medium">{workspace.osint_runs.length}</p></div>
        <div className="p-4 bg-white border rounded"><p className="text-xs text-zinc-400 uppercase">Analiz oturumu</p><p className="font-medium">{workspace.analyses.length}</p></div>
        <div className="p-4 bg-white border rounded">
          <p className="text-xs text-zinc-400 uppercase">Vaka Kimliği</p>
          <p className="font-medium text-sm font-mono">{caseId.slice(0, 8)}...</p>
        </div>
      </div>
      <div className="flex gap-3">
        <a
          href={`/osint`}
          className="px-4 py-2 text-sm bg-zinc-900 text-white rounded hover:bg-zinc-800"
        >
          Yeni OSINT Araştırması
        </a>
        <a
          href={`/analiz`}
          className="px-4 py-2 text-sm border rounded hover:bg-zinc-50"
        >
          Yeni Analiz
        </a>
      </div>
    </div>
  );
}

function OsintTab({ workspace }: { workspace: CaseWorkspace }) {
  if (!workspace.osint_runs.length) return <Empty label="Bu vakaya bağlı OSINT çalışması bulunmuyor." icon={<Search size={18} />} />;
  return <div className="space-y-3">{workspace.osint_runs.map((run) => {
    const findingCount = run.result?.coverage.unique_finding_count ?? run.result?.findings.length ?? 0;
    return <a key={run.id} href={`/osint/calisma/${run.id}`} className="block p-4 bg-white border rounded hover:border-zinc-400">
      <div className="flex items-center justify-between"><strong>{run.query}</strong><span className="text-xs text-zinc-500">{run.status}</span></div>
      <p className="text-sm text-zinc-500 mt-1">{run.query_type === "username" ? "Kullanıcı adı" : "Tam isim"} · {findingCount} benzersiz bulgu</p>
    </a>;
  })}</div>;
}

function AnalysesTab({ workspace }: { workspace: CaseWorkspace }) {
  if (!workspace.analyses.length) return <Empty label="Bu vakaya bağlı analiz oturumu bulunmuyor." icon={<FileText size={18} />} />;
  return <div className="space-y-3">{workspace.analyses.map((analysis) => <a key={analysis.id} href={`/analiz/oturum/${analysis.id}`} className="block p-4 bg-white border rounded hover:border-zinc-400">
    <div className="flex items-center justify-between"><strong>{analysis.title}</strong><span className="text-xs text-zinc-500">{analysis.status}</span></div>
    <p className="text-sm text-zinc-500 mt-1">{analysis.source_type} · {analysis.turns.length} konsey kaydı</p>
  </a>)}</div>;
}

function EvidenceTab({ workspace }: { workspace: CaseWorkspace }) {
  const evidence = useMemo(() => workspace.analyses.flatMap((analysis) => analysis.result?.evidence ?? []), [workspace.analyses]);
  if (!evidence.length) return <Empty label="Henüz analiz kanıtı kaydedilmemiş." icon={<FolderOpen size={18} />} />;
  return <div className="space-y-3">{evidence.map((item) => <div key={`${item.id}-${item.sha256}`} className="p-4 bg-white border rounded">
    <div className="flex items-center gap-2"><CheckCircle2 size={16} className="text-emerald-600" /><strong>{item.id}</strong></div>
    <p className="text-sm text-zinc-700 mt-2 whitespace-pre-wrap">{item.content}</p>
    <code className="text-[11px] text-zinc-400 break-all">SHA-256: {item.sha256}</code>
  </div>)}</div>;
}

function Empty({ label, icon }: { label: string; icon: ReactNode }) {
  return <div className="flex items-center gap-2 text-sm text-zinc-500 py-8">{icon}{label}</div>;
}
