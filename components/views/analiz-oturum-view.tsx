"use client";

import { useEffect, useState } from "react";
import { joeApi } from "@/lib/api";
import type { AnalysisSession } from "@/lib/types";
import { DurumRozeti, SayfaBasligi } from "@/components/ui";
import { LoaderCircle } from "lucide-react";

const PHASE_LABELS: Record<string, string> = {
  "hazırlanıyor": "Hazırlık",
  "bağımsız_görüşler": "Bağımsız Görüş",
  "çapraz_sorgu": "Çapraz Sorgu",
  "görüş_revizyonu": "Revizyon",
  "ortak_sentez": "Sentez",
  "tamamlandı": "Tamamlandı",
  "başarısız": "Başarısız",
};

export function AnalizOturumView({ analysisId }: { analysisId: string }) {
  const [session, setSession] = useState<AnalysisSession | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    joeApi.analysis(analysisId).then((s) => {
      setSession(s);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, [analysisId]);

  useEffect(() => {
    if (!session || !["queued", "running"].includes(session.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const updated = await joeApi.analysis(analysisId);
        setSession(updated);
      } catch { /* polling */ }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [session, analysisId]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <LoaderCircle className="animate-spin text-zinc-500" size={32} />
      </div>
    );
  }

  if (!session) {
    return <div className="p-8 text-zinc-500">Analiz bulunamadı.</div>;
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <SayfaBasligi
        eyebrow="ANALİZ KONSEYİ"
        title={session.title}
        description={`Durum: ${PHASE_LABELS[session.progress_phase || session.status] || session.status}`}
      />
      <div className="mt-4 flex items-center gap-3">
        <DurumRozeti durum={session.status} />
        <span className="text-sm text-zinc-500">
          Faz: {PHASE_LABELS[session.progress_phase || ""] || session.progress_phase || "—"}
        </span>
      </div>
      {session.error_message && (
        <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded text-red-700 text-sm">
          {session.error_message}
        </div>
      )}
      {session.status === "completed" && session.result && (
        <div className="mt-6">
          <h3 className="font-medium text-zinc-700 mb-2">Konsey Sentezi</h3>
          <div className="p-4 bg-white border rounded text-sm whitespace-pre-wrap">
            {session.result.synthesis?.executive_summary || "Sentez mevcut değil."}
          </div>
          {session.turns && session.turns.length > 0 && (
            <div className="mt-4 space-y-2">
              <h4 className="text-sm font-medium text-zinc-600">Persona Görüşleri ({session.turns.length})</h4>
              {session.turns.map((turn: any) => (
                <div key={turn.id} className="p-3 bg-white border rounded text-xs">
                  <span className="font-medium">{turn.persona_id}</span>
                  <span className="text-zinc-400 ml-2">· {turn.phase}</span>
                  <pre className="mt-1 text-zinc-600 whitespace-pre-wrap max-h-32 overflow-y-auto">
                    {JSON.stringify(turn.payload, null, 2).slice(0, 500)}
                  </pre>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
