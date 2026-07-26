"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { joeApi, readableError } from "@/lib/api";
import { analysisSessionPath } from "@/lib/analysis-path";
import type { AnalysisSession, AnalysisStage, CouncilTurn, Persona } from "@/lib/types";
import { DurumRozeti, SayfaBasligi } from "@/components/ui";
import { ArrowLeft, CheckCircle2, ChevronDown, CircleHelp, Clock3, LoaderCircle, Quote, ShieldCheck, XCircle } from "lucide-react";

const PHASE_LABELS: Record<string, string> = {
  "hazırlanıyor": "Hazırlık",
  "bağımsız_görüşler": "Bağımsız Görüş",
  "çapraz_sorgu": "Çapraz Sorgu",
  "görüş_revizyonu": "Revizyon",
  "ortak_sentez": "Sentez",
  "tamamlandı": "Tamamlandı",
  "başarısız": "Başarısız",
};

const PHASES = [
  ["hazırlanıyor", "Hazırlık"],
  ["bağımsız_görüşler", "Bağımsız görüş"],
  ["çapraz_sorgu", "Çapraz sorgu"],
  ["görüş_revizyonu", "Revizyon"],
  ["ortak_sentez", "Sentez"],
  ["tamamlandı", "Tamamlandı"],
] as const;

const TURN_PHASE: Record<string, string> = {
  "bağımsız_görüşler": "bağımsız_görüş",
  "çapraz_sorgu": "çapraz_sorgu",
  "görüş_revizyonu": "görüş_revizyonu",
  "ortak_sentez": "ortak_sentez",
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
  const [stages, setStages] = useState<AnalysisStage[]>([]);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    Promise.all([joeApi.analysis(analysisId), joeApi.personas(), joeApi.analysisStages(analysisId)])
      .then(([analysis, catalog, stageItems]) => {
        setSession(analysis);
        setPersonas(catalog);
        setStages(stageItems);
      })
      .catch((caught) => setError(readableError(caught)))
      .finally(() => setLoading(false));
  }, [analysisId]);

  useEffect(() => {
    if (!session || !["queued", "running"].includes(session.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const [updated, stageItems] = await Promise.all([
          joeApi.analysis(analysisId),
          joeApi.analysisStages(analysisId),
        ]);
        setSession(updated);
        setStages(stageItems);
        setNow(Date.now());
        setError(null);
      } catch (caught) {
        setError(readableError(caught));
      }
    }, 1500);
    return () => window.clearInterval(timer);
  }, [session, analysisId]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const personaById = useMemo(() => new Map(personas.map((item) => [item.id, item])), [personas]);

  if (loading) return <div className="flex items-center justify-center py-24"><LoaderCircle className="animate-spin text-zinc-500" size={32} /></div>;
  if (!session) return <div className="p-8 text-zinc-500">{error || "Analiz bulunamadı."}</div>;

  const outputSource = session.result?.final_persona_outputs ?? session.result?.independent_analyses ?? {};
  const personaIds = session.selected_personas.length ? session.selected_personas : Object.keys(outputSource);
  const returnHref = session.case_id ? `/vakalar/${session.case_id}` : "/analiz";
  const lastStagePhase = stages.length ? stages[stages.length - 1].phase : null;
  const activePhase = session.progress_phase === "başarısız" || session.progress_phase === "iptal edildi"
    ? lastStagePhase || "hazırlanıyor"
    : session.progress_phase || (session.status === "queued" ? "hazırlanıyor" : session.status);
  const phaseIndex = Math.max(0, PHASES.findIndex(([id]) => id === activePhase));
  const expectedIds = activePhase === "ortak_sentez" ? ["moderator"] : personaIds;
  const latestStageByPersona = new Map<string, AnalysisStage>();
  stages.filter((stage) => stage.phase === activePhase && stage.persona_id).forEach((stage) => latestStageByPersona.set(stage.persona_id || "", stage));
  const activeTurnPhase = TURN_PHASE[activePhase];
  const latestTurns = latestTurnsByKey(session.turns);
  const activeTurns = latestTurns.filter((turn) => !activeTurnPhase || turn.phase === activeTurnPhase);
  const completedCount = expectedIds.filter((id) => latestStageByPersona.get(id)?.status === "completed" || activeTurns.some((turn) => turn.persona_id === id)).length;
  const heartbeatAgo = session.heartbeat_at ? Math.max(0, Math.floor((now - new Date(session.heartbeat_at).getTime()) / 1000)) : null;
  const sessionId = session.id;

  async function cancelSession() {
    try {
      setSession(await joeApi.cancelAnalysis(sessionId));
      setError(null);
    } catch (caught) {
      setError(readableError(caught));
    }
  }

  async function retrySession() {
    try {
      const retried = await joeApi.retryAnalysis(sessionId);
      window.location.assign(analysisSessionPath(retried));
    } catch (caught) {
      setError(readableError(caught));
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <SayfaBasligi
        eyebrow="ANALİZ KONSEYİ"
        title={session.title}
        description={`Durum: ${PHASE_LABELS[session.progress_phase || session.status] || session.status}`}
        actions={<Link href={returnHref} className="ikincil-buton"><ArrowLeft size={16} /> Geri dön</Link>}
      />
      <div className="mt-4 flex items-center gap-3"><DurumRozeti durum={session.status} /><span className="text-sm text-zinc-500">Faz: {PHASE_LABELS[session.progress_phase || ""] || session.progress_phase || "—"}</span></div>

      <div className="mt-5 grid grid-cols-2 gap-px border border-zinc-200 bg-zinc-200 md:grid-cols-6">
        {PHASES.map(([id, label], index) => <div key={id} className={`bg-white px-3 py-3 text-xs ${index < phaseIndex ? "text-emerald-700" : index === phaseIndex ? "font-semibold text-zinc-900" : "text-zinc-400"}`}><span className="mr-2 inline-grid h-6 w-6 place-items-center border border-current">{index < phaseIndex ? "✓" : index + 1}</span>{label}</div>)}
      </div>

      {error ? <div className="mt-4 border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">Canlı durum yenilenemedi: {error}</div> : null}
      {session.error_message ? <div className="mt-4 border border-red-200 bg-red-50 p-3 text-sm text-red-700">{analysisErrorMessage(session.error_message)}</div> : null}

      {session.status === "failed" ? <section className="mt-6 border border-red-200 bg-white p-5 shadow-sm">
        <div className="flex items-start gap-3"><XCircle className="mt-0.5 shrink-0 text-red-600" size={20} /><div className="flex-1"><p className="eyebrow">OTURUM DURDU</p><h2 className="mt-1 text-lg font-semibold text-zinc-900">Sağlayıcı aşaması tamamlanamadı</h2><p className="mt-2 text-sm leading-6 text-zinc-600">Tamamlanan gerçek yanıtlar korunur. Başarısız görevler aşağıda açıkça gösterilir; Joe bunların yerine uydurma yanıt üretmez.</p></div><button type="button" className="ikincil-buton shrink-0" onClick={() => void retrySession()}>Tekrar sıraya al</button></div>
        <div className="mt-4 grid gap-2 sm:grid-cols-2">{Array.from(new Map(stages.filter((stage) => stage.persona_id).map((stage) => [`${stage.phase}:${stage.persona_id}`, stage])).values()).slice().reverse().map((stage) => {
          const persona = personaById.get(stage.persona_id || "");
          return <div key={`${stage.phase}:${stage.persona_id}`} className="border border-zinc-200 p-3"><div className="flex items-center justify-between gap-2"><strong className="text-sm text-zinc-800">{stage.persona_id === "moderator" ? "Konsey moderatörü" : persona?.name || stage.persona_id}</strong><span className={stage.status === "completed" ? "text-xs text-emerald-700" : "text-xs text-red-600"}>{stage.status === "completed" ? "Tamamlandı" : "Başarısız"}</span></div><small className="mt-1 block text-zinc-400">{PHASE_LABELS[stage.phase] || stage.phase.replaceAll("_", " ")}</small>{stage.error_message ? <p className="mt-2 text-xs leading-5 text-red-600">{analysisErrorMessage(stage.error_message)}</p> : null}</div>;
        })}</div>
        {latestTurns.length ? <div className="mt-5 space-y-3"><h3 className="text-sm font-semibold text-zinc-800">Durmadan önce kaydedilen yanıtlar</h3>{latestTurns.slice().reverse().map((turn) => <LiveTurn key={`${turn.phase}:${turn.persona_id}`} turn={turn} persona={personaById.get(turn.persona_id)} />)}</div> : null}
      </section> : null}

      {["queued", "running"].includes(session.status) ? <div className="mt-6 space-y-5">
        <section className="border border-zinc-200 bg-white p-5 shadow-sm">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div><p className="eyebrow">CANLI ÇALIŞMA DURUMU</p><h2 className="mt-1 text-xl font-semibold text-zinc-900">{session.status === "queued" ? "Oturum çalışma sırasında" : `${PHASE_LABELS[activePhase] || activePhase} yürütülüyor`}</h2><p className="mt-2 text-sm leading-6 text-zinc-600">{session.status === "queued" ? "Oturum kalıcı olarak kaydedildi. Önceki iş tamamlandığında worker bu konseyi otomatik başlatacak; sayfadan çıkabilirsin." : `${completedCount}/${expectedIds.length} görev tamamlandı. Her gerçek sağlayıcı yanıtı gelir gelmez aşağıya eklenir.`}</p></div>
            <div className="flex items-center gap-3 text-xs text-zinc-500"><Clock3 size={15} />{heartbeatAgo === null ? "Başlama bekleniyor" : heartbeatAgo < 5 ? "Şimdi etkin" : `Son etkinlik ${heartbeatAgo} sn önce`}<button type="button" className="ikincil-buton" onClick={() => void cancelSession()}>İptal et</button></div>
          </div>
          <div className="mt-4 h-2 overflow-hidden bg-zinc-100"><div className="h-full bg-emerald-700 transition-all" style={{ width: `${expectedIds.length ? Math.max(session.status === "running" ? 5 : 0, completedCount / expectedIds.length * 100) : 0}%` }} /></div>
          <p className="mt-3 text-xs text-zinc-400">Kalıcı oturum adresi: {typeof window !== "undefined" ? window.location.pathname : session.id}</p>
        </section>

        {session.status === "running" ? <section className="border border-zinc-200 bg-white p-5 shadow-sm">
          <div className="mb-4"><p className="eyebrow">GÖREVLER</p><h2 className="mt-1 text-lg font-semibold text-zinc-900">Konsey şu anda ne yapıyor?</h2></div>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{expectedIds.map((personaId) => {
            const stage = latestStageByPersona.get(personaId);
            const completedByTurn = activeTurns.some((turn) => turn.persona_id === personaId);
            const state = completedByTurn ? "completed" : stage?.status || "queued";
            const persona = personaById.get(personaId);
            return <div key={personaId} className="flex items-center gap-3 border border-zinc-200 p-3"><span className="grid h-8 w-8 place-items-center border text-xs font-semibold" style={{ borderColor: persona?.accent || "#64748b", color: persona?.accent || "#64748b" }}>{personaId === "moderator" ? "AI" : persona?.name.split(" ").map((part) => part[0]).slice(0, 2).join("") || personaId.slice(0, 2).toUpperCase()}</span><div className="min-w-0 flex-1"><strong className="block truncate text-sm text-zinc-800">{personaId === "moderator" ? "Konsey moderatörü" : persona?.name || personaId}</strong><small className={state === "failed" ? "text-red-600" : state === "completed" ? "text-emerald-700" : "text-zinc-500"}>{state === "running" ? "Yanıt üretiyor…" : state === "completed" ? "Tamamlandı" : state === "failed" ? "Yanıt alınamadı" : "Sırada"}</small></div>{state === "running" ? <LoaderCircle size={16} className="animate-spin text-zinc-400" /> : state === "completed" ? <CheckCircle2 size={16} className="text-emerald-700" /> : state === "failed" ? <XCircle size={16} className="text-red-600" /> : null}</div>;
          })}</div>
        </section> : null}

        <section className="border border-zinc-200 bg-white p-5 shadow-sm">
          <div className="mb-4"><p className="eyebrow">CANLI OTURUM KAYDI</p><h2 className="mt-1 text-lg font-semibold text-zinc-900">Tamamlanan gerçek yanıtlar</h2></div>
          {latestTurns.length ? <div className="space-y-3">{latestTurns.slice().reverse().map((turn) => <LiveTurn key={`${turn.phase}:${turn.persona_id}`} turn={turn} persona={personaById.get(turn.persona_id)} />)}</div> : <div className="flex min-h-28 items-center justify-center gap-3 border border-dashed border-zinc-200 text-sm text-zinc-500"><LoaderCircle size={18} className={session.status === "running" ? "animate-spin" : ""} />{session.status === "queued" ? "Worker sırası bekleniyor…" : "İlk sağlayıcı yanıtı bekleniyor…"}</div>}
        </section>
      </div> : null}

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

function latestTurnsByKey(turns: CouncilTurn[]): CouncilTurn[] {
  const latest = new Map<string, CouncilTurn>();
  turns.forEach((turn) => latest.set(`${turn.phase}:${turn.persona_id}`, turn));
  return Array.from(latest.values()).sort((left, right) => left.created_at.localeCompare(right.created_at));
}

function LiveTurn({ turn, persona }: { turn: CouncilTurn; persona?: Persona }) {
  const output = readOutput(turn.payload);
  const summary = output.revised_thesis || output.thesis || (turn.phase === "ortak_sentez" ? String((turn.payload as Record<string, unknown>).executive_summary || "Sentez tamamlandı.") : "Yapılandırılmış yanıt kaydedildi.");
  return <article className="flex gap-3 border border-zinc-200 p-3"><span className="mt-1 h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: persona?.accent || "#64748b" }} /><div><div className="flex flex-wrap items-center gap-2"><strong className="text-sm text-zinc-900">{turn.persona_id === "moderator" ? "Konsey moderatörü" : persona?.name || turn.persona_id}</strong><small className="text-xs text-zinc-400">{PHASE_LABELS[turn.phase] || turn.phase.replaceAll("_", " ")}</small></div><p className="mt-1 text-sm leading-6 text-zinc-600">{summary}</p></div></article>;
}

function analysisErrorMessage(message: string): string {
  if (message.toLocaleLowerCase("en-US").includes("insufficient balance")) {
    return "Sağlayıcı bakiyesi yetersiz. Bağlantının kredi/bakiye durumunu kontrol et.";
  }
  return message;
}
