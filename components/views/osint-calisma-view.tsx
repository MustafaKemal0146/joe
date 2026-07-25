"use client";

import { useEffect, useState } from "react";
import { joeApi } from "@/lib/api";
import type { OsintFinding, OsintRun } from "@/lib/types";
import { DurumRozeti, SayfaBasligi } from "@/components/ui";
import { LoaderCircle, ExternalLink } from "lucide-react";

export function OsintCalismaView({ runId }: { runId: string }) {
  const [run, setRun] = useState<OsintRun | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    joeApi.osintRun(runId).then((r) => {
      setRun(r);
      setLoading(false);
    });
  }, [runId]);

  useEffect(() => {
    if (!run || !["queued", "running"].includes(run.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const updated = await joeApi.osintRun(runId);
        setRun(updated);
      } catch { /* polling fallback */ }
    }, 1800);
    return () => window.clearInterval(timer);
  }, [run, runId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <LoaderCircle className="animate-spin text-zinc-500" size={32} />
      </div>
    );
  }

  if (!run) {
    return <div className="p-8 text-zinc-500">Çalışma bulunamadı.</div>;
  }

  const findings = run.result?.findings ?? [];

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <SayfaBasligi
        eyebrow="OSINT KEŞİF"
        title={`Araştırma: ${run.query}`}
        description={`Tür: ${run.query_type === "username" ? "Kullanıcı adı" : "Tam ad"} · Durum: ${run.status}`}
      />
      <div className="mt-4">
        <DurumRozeti durum={run.status} />
      </div>
      {run.error_code && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded text-red-700 text-sm">
          {run.error_message || run.error_code}
        </div>
      )}
      <div className="mt-6 space-y-2">
        <h3 className="font-medium text-zinc-700">Bulunan Bağlantılar ({findings.length})</h3>
        {findings.length === 0 && <p className="text-zinc-400 text-sm">Henüz bulgu yok.</p>}
        {findings.map((f: OsintFinding, i: number) => (
          <div key={i} className="p-3 bg-white border rounded flex items-start gap-3">
            <div className="flex-1 min-w-0">
              <a
                href={f.profile_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-600 hover:underline text-sm break-all flex items-center gap-1"
              >
                {f.profile_url} <ExternalLink size={12} />
              </a>
              <div className="text-xs text-zinc-500 mt-1">
                {f.site} · {f.username || "—"} · Kaynak: {(f.sources || []).join(", ")}
              </div>
            </div>
            <span className="text-xs px-2 py-0.5 rounded bg-zinc-100 text-zinc-600 shrink-0">
              {f.identity_status || "doğrulanmadı"}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
