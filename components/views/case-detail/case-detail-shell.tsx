"use client";

import { useEffect, useState } from "react";
import { joeApi } from "@/lib/api";
import type { CaseRecord } from "@/lib/types";
import { LoaderCircle } from "lucide-react";

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
      <div className="mb-6">
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

      {/* Sekmeler */}
      <CaseDetailTabs caseId={caseId} caseName={caseRecord.name} />
    </div>
  );
}

function CaseDetailTabs({ caseId, caseName }: { caseId: string; caseName: string }) {
  const [activeTab, setActiveTab] = useState("overview");

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
        {activeTab === "overview" && <OverviewTab caseId={caseId} caseName={caseName} />}
        {activeTab === "osint" && <div className="text-sm text-zinc-500">OSINT çalışmaları burada listelenecek.</div>}
        {activeTab === "analyses" && <div className="text-sm text-zinc-500">Analiz oturumları burada listelenecek.</div>}
        {activeTab === "evidence" && <div className="text-sm text-zinc-500">Kanıt kayıtları burada listelenecek.</div>}
      </div>
    </div>
  );
}

function OverviewTab({ caseId, caseName }: { caseId: string; caseName: string }) {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <div className="p-4 bg-white border rounded">
          <p className="text-xs text-zinc-400 uppercase">Vaka Adı</p>
          <p className="font-medium">{caseName}</p>
        </div>
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
