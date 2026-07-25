"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { joeApi } from "@/lib/api";
import type { AnalysisSession, Persona } from "@/lib/types";
import { DurumRozeti, SayfaBasligi } from "@/components/ui";
import { ArrowLeft, ChevronDown, CircleHelp, LoaderCircle, Quote, ShieldCheck } from "lucide-react";

const PHASE_LABELS: Record<string, string> = {
  "hazırlanıyor": "Hazırlık",
  "bağımsız_görüşler": "Bağımsız Görüş",
  "çapraz_sorgu": "Çapraz Sorgu",
  "görüş_revizyonu": "Revizyon",
  "ortak_sentez": "Sentez",
  "tamamlandı": "Tamamlandı",
  "başarısız": "Başarısız",
};

type Observation = {
  claim?: string;
  interpretation?: string;
  evidence_ids?: string[];
  alternatives?: string[];
  confidence?: string;
};

type PersonaOutput = {
  thesis?: string;
  revised_thesis?: string;
  observations?: Observation[];
  revised_observations?: Observation[];
  tensions?: string[];
  unknowns?: string[];
  abstentions?: string[];
};

function readOutput(value: unknown): PersonaOutput {
  if (!value || typeof value !== "object") return {};
  const record = value as Record<string, unknown>;
  const nested = record.payload && typeof record.payload === "object" ? record.payload as Record<string, unknown> : record;
  return nested as PersonaOutput;
}

function values(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

export function AnalizOturumView({ analysisId }: { analysisId: string }) {
  const [session, setSession] = useState<AnalysisSession | null>(null);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([joeApi.analysis(analysisId), joeApi.personas()])
      .then(([analysis, catalog]) => {
        setSession(analysis);
        setPersonas(catalog);
      })
      .finally(() => setLoading(false));
  }, [analysisId]);

  useEffect(() => {
    if (!session || !["queued", "running"].includes(session.status)) return;
    const timer = window.setInterval(async () => {
      try {
        setSession(await joeApi.analysis(analysisId));
      } catch {
        // Geçici ağ hatasında önceki güvenilir durum korunur.
      }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [session, analysisId]);

  const personaById = useMemo(() => new Map(personas.map((item) => [item.id, item])), [personas]);

  if (loading) return <div className="flex items-center justify-center py-24"><LoaderCircle className="animate-spin text-zinc-500" size={32} /></div>;
  if (!session) return <div className="p-8 text-zinc-500">Analiz bulunamadı.</div>;

  const outputSource = session.result?.final_persona_outputs ?? session.result?.independent_analyses ?? {};
  const personaIds = session.selected_personas.length ? session.selected_personas : Object.keys(outputSource);
  const returnHref = session.case_id ? `/vakalar/${session.case_id}` : "/analiz";

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <SayfaBasligi
        eyebrow="ANALİZ KONSEYİ"
        title={session.title}
        description={`Durum: ${PHASE_LABELS[session.progress_phase || session.status] || session.status}`}
        actions={<Link href={returnHref} className="ikincil-buton"><ArrowLeft size={16} /> Geri dön</Link>}
      />
      <div className="mt-4 flex items-center gap-3"><DurumRozeti durum={session.status} /><span className="text-sm text-zinc-500">Faz: {PHASE_LABELS[session.progress_phase || ""] || session.progress_phase || "—"}</span></div>

      {session.error_message ? <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded text-red-700 text-sm">{session.error_message}</div> : null}

      {session.status === "completed" && session.result ? (
        <div className="mt-7 space-y-7">
          <section className="rounded-xl border border-zinc-200 bg-white p-5 shadow-sm">
            <div className="flex items-center gap-2 text-sm font-semibold text-zinc-800"><ShieldCheck size={18} className="text-emerald-700" /> Konsey sentezi</div>
            <p className="mt-3 text-[15px] leading-7 text-zinc-700 whitespace-pre-wrap">{session.result.synthesis?.executive_summary || "Sentez mevcut değil."}</p>
          </section>

          <section>
            <div className="mb-3"><p className="eyebrow">KURAMSAL GÖRÜŞLER</p><h2 className="text-xl font-semibold text-zinc-900">Uzman değerlendirmeleri</h2><p className="mt-1 text-sm text-zinc-500">Her kart, ilgili kuramsal merceğin kanıta dayalı ve revize edilmiş yorumunu gösterir.</p></div>
            <div className="space-y-4">
              {personaIds.map((personaId) => <PersonaCard key={personaId} persona={personaById.get(personaId)} personaId={personaId} output={readOutput(outputSource[personaId])} />)}
            </div>
          </section>
        </div>
      ) : null}
    </div>
  );
}

function PersonaCard({ persona, personaId, output }: { persona?: Persona; personaId: string; output: PersonaOutput }) {
  const thesis = output.revised_thesis || output.thesis || "Bu persona için gösterilebilir bir yorum üretilmedi.";
  const observations = output.revised_observations?.length ? output.revised_observations : output.observations ?? [];
  const unknowns = values(output.unknowns);
  const abstentions = values(output.abstentions);
  const tensions = values(output.tensions);

  return <article className="overflow-hidden rounded-xl border border-zinc-200 bg-white shadow-sm">
    <header className="border-l-4 px-5 py-4" style={{ borderLeftColor: persona?.accent || "#64748b" }}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h3 className="text-lg font-semibold text-zinc-900">{persona?.name || personaId}</h3><p className="mt-0.5 text-sm text-zinc-600">{persona?.title || "Kuramsal değerlendirme"}</p></div>
        {persona?.group ? <span className="rounded-full bg-zinc-100 px-2.5 py-1 text-xs text-zinc-600">{persona.group}</span> : null}
      </div>
      {persona?.concepts?.length ? <div className="mt-3 flex flex-wrap gap-1.5">{persona.concepts.slice(0, 4).map((concept) => <span key={concept} className="rounded-md bg-zinc-50 px-2 py-1 text-xs text-zinc-500">{concept}</span>)}</div> : null}
    </header>
    <div className="space-y-5 p-5">
      <div><div className="flex items-center gap-2 text-sm font-semibold text-zinc-800"><Quote size={16} /> Uzman yorumu</div><p className="mt-2 leading-7 text-zinc-700">{thesis}</p></div>
      {observations.length ? <div><h4 className="text-sm font-semibold text-zinc-800">Kanıta dayalı gözlemler</h4><div className="mt-2 space-y-3">{observations.map((observation, index) => <div key={index} className="rounded-lg bg-zinc-50 p-3"><p className="text-sm font-medium text-zinc-800">{observation.claim || "Gözlem"}</p>{observation.interpretation ? <p className="mt-1 text-sm leading-6 text-zinc-600">{observation.interpretation}</p> : null}<div className="mt-2 flex flex-wrap gap-1.5">{values(observation.evidence_ids).map((id) => <span key={id} className="rounded bg-white px-1.5 py-0.5 font-mono text-[11px] text-zinc-600 ring-1 ring-zinc-200">{id}</span>)}{observation.confidence ? <span className="rounded bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-800">Güven: {observation.confidence}</span> : null}</div>{values(observation.alternatives).length ? <p className="mt-2 text-xs leading-5 text-zinc-500">Alternatif: {values(observation.alternatives).join(" ")}</p> : null}</div>)}</div></div> : null}
      {(tensions.length || unknowns.length || abstentions.length) ? <details className="group rounded-lg border border-zinc-200"><summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2.5 text-sm font-medium text-zinc-700">Sınırlar ve belirsizlikler <ChevronDown size={16} className="transition-transform group-open:rotate-180" /></summary><div className="space-y-3 border-t px-3 py-3 text-sm text-zinc-600">{tensions.length ? <NoteList label="Gerilimler" items={tensions} /> : null}{unknowns.length ? <NoteList label="Eksik bağlam" items={unknowns} /> : null}{abstentions.length ? <NoteList label="Çekimser kalınan noktalar" items={abstentions} /> : null}</div></details> : null}
    </div>
  </article>;
}

function NoteList({ label, items }: { label: string; items: string[] }) {
  return <div><p className="mb-1 flex items-center gap-1.5 font-medium text-zinc-700"><CircleHelp size={14} /> {label}</p><ul className="list-disc space-y-1 pl-5 leading-6">{items.map((item, index) => <li key={index}>{item}</li>)}</ul></div>;
}
