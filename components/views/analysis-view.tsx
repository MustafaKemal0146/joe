"use client";

import {
  AlertTriangle,
  ArrowRight,
  BrainCircuit,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleDotDashed,
  ClipboardCheck,
  FileArchive,
  FileText,
  FolderSearch,
  Layers3,
  LoaderCircle,
  MessageSquareMore,
  Paperclip,
  Quote,
  RotateCcw,
  Scale,
  ShieldCheck,
  Upload,
  UsersRound,
  X,
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { joeApi, readableError } from "../../lib/api";
import { analysisSessionPath } from "../../lib/analysis-path";
import type {
  AnalysisSession,
  CaseRecord,
  ImportBatch,
  ImportedConversation,
  ImportedMessage,
  Gorunum,
  Persona,
  ProviderConnection,
  ProviderProfile,
  SynthesisClaim,
} from "../../lib/types";
import { DurumRozeti, HataKutusu, SayfaBasligi } from "../ui";
import { DirectorySource } from "./directory-source";


const CORE_COUNCIL = ["freud", "jung", "klein", "reich", "fromm", "kristeva", "zizek"];
const MAX_ANALYSIS_SOURCE_CHARS = 1_000_000;
const PHASES = [
  ["hazırlanıyor", "Hazırlık"],
  ["bağımsız_görüşler", "Bağımsız görüş"],
  ["çapraz_sorgu", "Çapraz sorgu"],
  ["görüş_revizyonu", "Revizyon"],
  ["ortak_sentez", "Sentez"],
  ["tamamlandı", "Tamamlandı"],
] as const;


export function AnalysisView({
  personas,
  connections,
  profiles,
  cases,
  onNavigate,
}: {
  personas: Persona[];
  connections: ProviderConnection[];
  profiles: ProviderProfile[];
  cases: CaseRecord[];
  onNavigate: (view: Gorunum) => void;
}) {
  const [title, setTitle] = useState("");
  const [sourceText, setSourceText] = useState("");
  const [sourceType, setSourceType] = useState("text");
  const [sourcePanel, setSourcePanel] = useState<"content" | "whatsapp" | "instagram">("content");
  const [directoryOpen, setDirectoryOpen] = useState(false);
  const [whatsappTarget, setWhatsappTarget] = useState("");
  const [whatsappParticipants, setWhatsappParticipants] = useState<string[]>([]);
  const [instagramImports, setInstagramImports] = useState<ImportBatch[]>([]);
  const [instagramConversations, setInstagramConversations] = useState<ImportedConversation[]>([]);
  const [instagramMessages, setInstagramMessages] = useState<ImportedMessage[]>([]);
  const [instagramBatchId, setInstagramBatchId] = useState("");
  const [instagramConversationId, setInstagramConversationId] = useState("");
  const [instagramTarget, setInstagramTarget] = useState("");
  const [instagramLoading, setInstagramLoading] = useState(false);
  const [caseId, setCaseId] = useState("");
  const [selectedPersonas, setSelectedPersonas] = useState<string[]>([]);
  const [defaultConnection, setDefaultConnection] = useState("");
  const [synthesisConnection, setSynthesisConnection] = useState("");
  const [advancedRoutes, setAdvancedRoutes] = useState(false);
  const [routes, setRoutes] = useState<Record<string, string>>({});
  const [uploading, setUploading] = useState(false);
  const [uploadedName, setUploadedName] = useState<string | null>(null);
  const [artifactId, setArtifactId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<AnalysisSession | null>(null);
  const [resultTab, setResultTab] = useState<"sonuc" | "konsey" | "kanit">("sonuc");
  const [followup, setFollowup] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const whatsappFileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!session || !["queued", "running"].includes(session.status)) return;
    const timer = window.setInterval(async () => {
      try {
        const updated = await joeApi.analysis(session.id);
        setSession(updated);
        if (updated.status === "completed") setResultTab("sonuc");
      } catch (caught) {
        setError(readableError(caught));
      }
    }, 1800);
    return () => window.clearInterval(timer);
  }, [session]);

  const groups = useMemo(() => {
    const result = new Map<string, Persona[]>();
    personas.forEach((persona) => {
      result.set(persona.group, [...(result.get(persona.group) ?? []), persona]);
    });
    return Array.from(result.entries());
  }, [personas]);

  const effectiveDefaultConnection = defaultConnection || connections[0]?.id || "";
  const selectedConnection = connections.find((item) => item.id === effectiveDefaultConnection);
  const selectedProfile = profiles.find((item) => item.id === selectedConnection?.provider_id);
  const analysisText = sourceType === "whatsapp" && whatsappTarget
    ? `ANALİZ ODAĞI: ${whatsappTarget}\n\nAşağıdaki WhatsApp konuşmasında yalnızca “${whatsappTarget}” adlı katılımcının söylem örüntülerini incele. Diğer katılımcılar bağlam sağlar; onlar hakkında çıkarım yapma.\n\n${sourceText}`
    : sourceType === "instagram" && instagramTarget
      ? `ANALİZ ODAĞI: ${instagramTarget}\n\nAşağıdaki Instagram konuşmasında yalnızca “${instagramTarget}” adlı katılımcının söylem örüntülerini incele. Diğer katılımcılar bağlam sağlar; onlar hakkında çıkarım yapma.\n\n${sourceText}`
    : sourceText;

  function togglePersona(id: string) {
    setSelectedPersonas((current) =>
      current.includes(id) ? current.filter((item) => item !== id) : [...current, id],
    );
  }

  async function uploadFile(file: File) {
    setUploading(true);
    setError(null);
    try {
      const artifact = await joeApi.uploadArtifact(file, caseId || undefined);
      setArtifactId(artifact.id);
      setSourceText(artifact.extracted_text ?? "");
      setSourceType(artifact.media_type.startsWith("image/") ? "visual" : artifact.extractor);
      setSourcePanel("content");
      setUploadedName(artifact.original_name);
      if (!title) setTitle(artifact.original_name.replace(/\.[^.]+$/, "") + " analizi");
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setUploading(false);
    }
  }

  async function loadWhatsApp(file: File) {
    const content = await file.text();
    const people = Array.from(new Set(Array.from(content.matchAll(/^\d{1,2}\.\d{1,2}\.\d{4}\s+\d{1,2}:\d{2}\s+-\s+([^:\n]+):/gm)).map((match) => match[1].trim())));
    setSourceText(content);
    setSourceType("whatsapp");
    setWhatsappParticipants(people);
    setWhatsappTarget(people[0] || "");
    setUploadedName(file.name);
    if (!title) setTitle(file.name.replace(/\.[^.]+$/, "") + " konuşma analizi");
  }

  async function loadInstagramImports() {
    setInstagramLoading(true);
    setError(null);
    try {
      const imports = await joeApi.imports();
      setInstagramImports(imports.filter((item) => item.case_id === caseId && item.source_type === "instagram" && item.status === "completed"));
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setInstagramLoading(false);
    }
  }

  async function selectInstagramImport(batchId: string) {
    setInstagramBatchId(batchId);
    setInstagramConversationId("");
    setInstagramConversations([]);
    setInstagramMessages([]);
    setInstagramTarget("");
    setSourceText("");
    if (!batchId) return;
    setInstagramLoading(true);
    try {
      setInstagramConversations(await joeApi.importConversations(batchId));
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setInstagramLoading(false);
    }
  }

  async function selectInstagramConversation(conversationId: string) {
    setInstagramConversationId(conversationId);
    setInstagramMessages([]);
    setInstagramTarget("");
    setSourceText("");
    if (!conversationId || !instagramBatchId) return;
    setInstagramLoading(true);
    try {
      // API sayfa başına en fazla 1.000 mesaj döndürür. Konuşmanın son 1.000
      // mesajını sessizce seçmek yerine tüm sayfaları alırız; kaynak sınırı
      // aşılırsa kullanıcıya açıkça bildirilir ve hiçbir bölüm kaybolmaz.
      const messages: ImportedMessage[] = [];
      const pageSize = 1000;
      for (let offset = 0; ; offset += pageSize) {
        const page = await joeApi.importMessages(instagramBatchId, conversationId, pageSize, offset);
        messages.push(...page);
        if (page.length < pageSize) break;
      }
      const conversation = instagramConversations.find((item) => item.id === conversationId);
      const ordered = [...messages].sort((left, right) => left.timestamp_ms - right.timestamp_ms);
      setInstagramMessages(ordered);
      setInstagramTarget(conversation?.participant_names[0] ?? "");
      setSourceText(ordered.map((item) => {
        const time = new Date(item.timestamp_ms).toLocaleString("tr-TR");
        const detail = item.content ?? (item.has_media ? "<Medya içeriği>" : "<Metin içeriği yok>");
        return `${time} — ${item.sender_name}: ${detail}`;
      }).join("\n"));
      setSourceType("instagram");
      if (!title && conversation?.title) setTitle(`${conversation.title} konuşma analizi`);
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setInstagramLoading(false);
    }
  }

  async function startAnalysis(event: FormEvent) {
    event.preventDefault();
    setError(null);
    if (selectedPersonas.length < 2) {
      setError("Konsey için en az iki persona seçmelisin.");
      return;
    }
    if (sourceType === "whatsapp" && !whatsappTarget) {
      setError("WhatsApp konuşması için analiz edilecek katılımcıyı seçmelisin.");
      return;
    }
    if (sourceType === "instagram" && (!instagramConversationId || !instagramTarget)) {
      setError("Instagram konuşması için konuşmayı ve analiz edilecek katılımcıyı seçmelisin.");
      return;
    }
    if (!effectiveDefaultConnection && Object.keys(routes).length === 0) {
      setError("Konseyin çalışması için bir AI sağlayıcı bağlantısı seçmelisin.");
      return;
    }
    setSubmitting(true);
    try {
      const selectedRoutes = advancedRoutes
        ? Object.fromEntries(
            Object.entries(routes).filter(([personaId, connectionId]) =>
              selectedPersonas.includes(personaId) && Boolean(connectionId),
            ),
          )
        : {};
      const created = await joeApi.createAnalysis({
        title: title || "Kuramsal konsey analizi",
        case_id: caseId,
        source_text: analysisText,
        source_type: sourceType,
        artifact_ids: artifactId ? [artifactId] : [],
        selected_personas: selectedPersonas,
        default_provider_connection_id: effectiveDefaultConnection || null,
        synthesis_provider_connection_id: synthesisConnection || effectiveDefaultConnection || null,
        provider_routes: selectedRoutes,
      });
      window.location.assign(analysisSessionPath(created));
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  async function askFollowup() {
    if (!session?.result || !followup.trim()) return;
    setSubmitting(true);
    setError(null);
    const evidence = session.result.evidence.map((item) => `[${item.id}] ${item.content}`).join("\n\n");
    const nextSource = `${evidence}\n\nÖNCEKİ KONSEY ÖZETİ:\n${session.result.synthesis.executive_summary}\n\nKULLANICININ TAKİP SORUSU:\n${followup.trim()}`;
    try {
      const created = await joeApi.createAnalysis({
        title: `Takip sorusu: ${followup.trim().slice(0, 80)}`,
        case_id: session.case_id,
        source_text: nextSource,
        source_type: "council_followup",
        selected_personas: session.selected_personas,
        default_provider_connection_id: session.default_provider_connection_id,
        synthesis_provider_connection_id: session.synthesis_provider_connection_id || session.default_provider_connection_id,
        provider_routes: session.provider_routes,
      });
      setFollowup("");
      window.location.assign(analysisSessionPath(created));
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  function reset() {
    setSession(null);
    setError(null);
    setResultTab("sonuc");
  }

  if (session) {
    return (
      <AnalysisRunView
        session={session}
        personas={personas}
        resultTab={resultTab}
        onTab={setResultTab}
        onReset={reset}
        followup={followup}
        onFollowup={setFollowup}
        onAskFollowup={() => void askFollowup()}
        submitting={submitting}
        error={error}
      />
    );
  }

  return (
    <div>
      <SayfaBasligi
        eyebrow="BAĞIMSIZ MODÜL / ANALİZ"
        title="Kuramsal Analiz Konseyi"
        description="Kaynağını ver, düşünce merceklerini kendin seç; görüşlerini bağımsız üretsinler, birbirlerini eleştirsinler ve kanıtlı bir senteze ulaşsınlar."
        actions={<span className="ayrim-etiketi analiz"><BrainCircuit size={16} /> OSINT’ten ayrı</span>}
      />

      <form onSubmit={startAnalysis} className="analiz-kurucu">
        <section className="panel kaynak-paneli">
          <div className="panel-baslik">
            <div><span className="adim-etiketi">01</span><span className="eyebrow">ANALİZ KAYNAĞI</span><h2>İncelenecek içeriği ekle</h2></div>
            <span className="karakter-sayaci">{sourceText.length.toLocaleString("tr-TR")} karakter</span>
          </div>

          <div className="kaynak-tipleri">
            <button type="button" className={sourcePanel === "content" ? "aktif" : ""} onClick={() => { setSourcePanel("content"); setSourceType("text"); }}><FileText size={16} /> İçerik</button>
            <button type="button" className={sourcePanel === "whatsapp" ? "aktif" : ""} onClick={() => { setSourcePanel("whatsapp"); setSourceType("whatsapp"); }}><MessageSquareMore size={16} /> WhatsApp</button>
            <button type="button" className={sourcePanel === "instagram" ? "aktif" : ""} onClick={() => { setSourcePanel("instagram"); setSourceType("instagram"); }}><FileArchive size={16} /> Instagram</button>
            <input
              ref={fileRef}
              hidden
              type="file"
              accept=".txt,.md,.json,.zip,.pdf,.docx,.png,.jpg,.jpeg,.webp"
              onChange={(event) => {
                const file = event.target.files?.[0];
                if (file) void uploadFile(file);
                event.currentTarget.value = "";
              }}
            />
          </div>

          {sourcePanel === "content" && uploadedName ? (
             <div className="yuklenen-dosya"><Paperclip size={16} /><div><strong>{uploadedName}</strong><span>{sourceText.trim() ? "Gerçek içerik çıkarıldı; istersen düzenleyebilirsin." : "Görsel kaynak hazır; seçili sağlayıcı vision desteğiyle incelenecek."}</span></div><button type="button" onClick={() => { setUploadedName(null); setArtifactId(null); setSourceText(""); setSourceType("text"); }} aria-label="Dosyayı kaldır"><X size={16} /></button></div>
          ) : null}

          {sourcePanel === "whatsapp" ? (
            <div className="source-workflow">
              <div className="source-workflow-head"><span className="source-step">01</span><div><strong>WhatsApp konuşmasını içe aktar</strong><p>`.txt` dışa aktarımını seç. Joe katılımcıları çıkarır; analiz odağını sen belirlersin.</p></div></div>
              <div className="source-dropzone">
                <input ref={whatsappFileRef} className="source-file-input" type="file" accept=".txt,text/plain" onChange={(event) => { const file = event.target.files?.[0]; if (file) void loadWhatsApp(file); event.currentTarget.value = ""; }} />
                <span className="source-dropzone-icon"><Upload size={18} /></span><span><strong>WhatsApp dosyanı seç</strong><small>Yalnızca `.txt` · içerik yerelde işlenir</small></span><button type="button" className="birincil-buton" onClick={() => whatsappFileRef.current?.click()}>Dosya seç</button>
              </div>
              {whatsappParticipants.length ? <div className="source-ready"><CheckCircle2 size={17} /><div><strong>{uploadedName || "Konuşma"} hazır</strong><small>{whatsappParticipants.length} katılımcı bulundu. Analiz odağını seç.</small></div><label><span>Analiz odağı</span><select value={whatsappTarget} onChange={(event) => setWhatsappTarget(event.target.value)}><option value="">Katılımcı seç</option>{whatsappParticipants.map((person) => <option key={person} value={person}>{person}</option>)}</select></label></div> : <div className="source-hint">Dosya seçildiğinde katılımcılar burada görünür; ham sohbet sessizce kesilmez.</div>}
              {sourceText ? <textarea className="analiz-metni" maxLength={MAX_ANALYSIS_SOURCE_CHARS} value={sourceText} onChange={(event) => setSourceText(event.target.value)} /> : null}
            </div>
          ) : sourcePanel === "instagram" ? (
            <div className="source-workflow">
              <div className="source-workflow-head"><span className="source-step">01</span><div><strong>Vakadaki Instagram arşivinden seç</strong><p>Tamamlanmış içe aktarmadaki gerçek konuşmayı seç; kişi ve bağlam ayrı ayrı görünür.</p></div></div>
              {!caseId ? <div className="source-empty"><FolderSearch size={20} /><div><strong>Önce vaka seç</strong><small>Instagram arşivleri vaka dosyasına bağlıdır; seçtiğin vakaya ait içe aktarımlar aşağıda açılır.</small></div><label className="source-case-picker"><span>Vaka seç</span><select value={caseId} onChange={(event) => setCaseId(event.target.value)}><option value="">Vaka seç</option>{cases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label></div> : <>
                <div className="source-case-status"><span>VAKA BAĞLAMI</span><strong>{cases.find((item) => item.id === caseId)?.name || "Seçili vaka"}</strong><button className="buton-ikincil" type="button" onClick={() => void loadInstagramImports()} disabled={instagramLoading}>{instagramLoading ? "Arşivler yükleniyor…" : "Arşivleri getir"}</button></div>
                {instagramImports.length ? <div className="source-select-grid"><label><span>İçe aktarma</span><select value={instagramBatchId} onChange={(event) => void selectInstagramImport(event.target.value)}><option value="">Arşiv seç</option>{instagramImports.map((item) => <option key={item.id} value={item.id}>{item.archive_owner_display_name || item.archive_owner_username || "Instagram arşivi"} — {item.total_conversations} konuşma</option>)}</select></label>{instagramConversations.length ? <label><span>Konuşma</span><select value={instagramConversationId} onChange={(event) => void selectInstagramConversation(event.target.value)}><option value="">Konuşma seç</option>{instagramConversations.map((item) => <option key={item.id} value={item.id}>{item.title || item.participant_names.join(", ") || "Adsız konuşma"} — {item.message_count} mesaj</option>)}</select></label> : null}{instagramConversationId ? <label><span>Analiz odağı</span><select value={instagramTarget} onChange={(event) => setInstagramTarget(event.target.value)}><option value="">Katılımcı seç</option>{(instagramConversations.find((item) => item.id === instagramConversationId)?.participant_names ?? []).map((person) => <option key={person} value={person}>{person}</option>)}</select></label> : null}</div> : <div className="source-hint">Bu vakada tamamlanmış bir Instagram içe aktarması görünmüyor. Vaka dosyasından arşiv ekleyebilirsin.</div>}
                {instagramMessages.length ? <div className="source-ready"><CheckCircle2 size={17} /><div><strong>{instagramMessages.length.toLocaleString("tr-TR")} gerçek mesaj hazır</strong><small>Seçilen kişi için konuşma sırası ve bağlam korunarak kanıt parçaları oluşturulacak.</small></div></div> : null}
              </>}
            </div>
          ) : (
            <>
              <textarea
                className="analiz-metni"
                maxLength={MAX_ANALYSIS_SOURCE_CHARS}
                value={sourceText}
                onChange={(event) => setSourceText(event.target.value)}
                placeholder="Sohbeti, metni veya çözümlemek istediğin içeriği buraya ekle. Joe içeriği K1, K2… kanıt parçalarına ayıracak; kaynak sessizce kesilmez."
              />
              <div className="kaynak-alt-bilgi">
                <span><ShieldCheck size={15} /> Yüklenen ham dosya yerel artefakt deposunda tutulur.</span>
                <div className="kaynak-aksiyonlari">
                  <button type="button" onClick={() => fileRef.current?.click()} disabled={uploading}>
                  {uploading ? <LoaderCircle size={15} className="donen" /> : <Upload size={15} />}
                  {uploading ? "Gerçek içerik çıkarılıyor…" : "Dosya seç"}
                  </button>
                  <button type="button" onClick={() => setDirectoryOpen((open) => !open)}><FolderSearch size={15} /> {directoryOpen ? "Dizin alanını kapat" : "Dizini indeksle"}</button>
                </div>
              </div>
              {directoryOpen ? <DirectorySource
                caseId={caseId}
                onPrepared={(text, nextTitle) => {
                  setSourceText(text);
                  setSourceType("local_corpus");
                  setUploadedName("Dizin indeksinden seçilen içerik");
                  setTitle((current) => current || nextTitle);
                  setDirectoryOpen(false);
                }}
              /> : null}
            </>
          )}
        </section>

        <section className="panel persona-paneli">
          <div className="panel-baslik">
            <div><span className="adim-etiketi">02</span><span className="eyebrow">KONSEY KADROSU</span><h2>Perspektifleri sen seç</h2></div>
            <div className="persona-hizli-aksiyonlar">
              <button type="button" onClick={() => setSelectedPersonas(CORE_COUNCIL.filter((id) => personas.some((persona) => persona.id === id)))}>Önerilen konsey</button>
              <button type="button" onClick={() => setSelectedPersonas(personas.map((persona) => persona.id))}>Tümünü seç</button>
              <button type="button" onClick={() => setSelectedPersonas([])}>Temizle</button>
              <span className="sayac">{selectedPersonas.length}</span>
            </div>
          </div>

          <div className="persona-gruplari">
            {groups.map(([group, members]) => (
              <div className="persona-grubu" key={group}>
                <div className="persona-grup-basligi"><span>{group}</span><i /></div>
                <div className="persona-grid">
                  {members.map((persona) => {
                    const selected = selectedPersonas.includes(persona.id);
                    return (
                      <button
                        type="button"
                        key={persona.id}
                        className={`persona-karti ${selected ? "secili" : ""}`}
                        onClick={() => togglePersona(persona.id)}
                        style={{ "--persona": persona.accent } as React.CSSProperties}
                        aria-pressed={selected}
                      >
                        <span className="persona-secim">{selected ? <Check size={13} /> : null}</span>
                        <span className="persona-monogram">{persona.name.split(" ").map((part) => part[0]).slice(0, 2).join("")}</span>
                        <span className="persona-kimlik"><strong>{persona.name}</strong><small>{persona.title}</small></span>
                        <p>{persona.summary}</p>
                        <span className="persona-kavramlar">{persona.concepts.slice(0, 3).map((concept) => <i key={concept}>{concept}</i>)}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </section>

        <section className="panel konsey-ayarlari">
          <div className="panel-baslik">
            <div><span className="adim-etiketi">03</span><span className="eyebrow">AI VE OTURUM</span><h2>Konsey bağlantısını ayarla</h2></div>
            <Layers3 size={21} />
          </div>

          <div className="tam-konsey-bandi">
            <BrainCircuit size={19} />
            <div><strong>Tam konsey protokolü</strong><span>Bağımsız görüş → çapraz sorgu → görüş revizyonu → kanıtlı ortak sentez</span></div>
          </div>

          <div className="form-grid iki analiz-meta-formu">
            <label><span>Oturum başlığı</span><input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Örn. İletişim örüntüsü analizi" /></label>
            <label><span>Vaka bağlantısı</span><select required value={caseId} onChange={(event) => setCaseId(event.target.value)}><option value="">Vaka seç</option>{cases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
            <label className="tam-satir"><span>Varsayılan AI bağlantısı</span><select value={effectiveDefaultConnection} onChange={(event) => setDefaultConnection(event.target.value)}><option value="">Sağlayıcı seç</option>{connections.map((item) => <option key={item.id} value={item.id}>{item.label} — {item.model}</option>)}</select></label>
            <label className="tam-satir"><span>Konsey moderatörü (genel açıklama)</span><select value={synthesisConnection || effectiveDefaultConnection} onChange={(event) => setSynthesisConnection(event.target.value)}><option value="">Varsayılan bağlantıyı kullan</option>{connections.map((item) => <option key={item.id} value={item.id}>{item.label} — {item.model}</option>)}</select><small>Bu model tüm persona görüşlerini, itirazları ve revizyonları okuyup en üstteki genel açıklamayı üretir.</small></label>
          </div>

          {connections.length === 0 ? (
            <div className="saglayici-uyarisi"><AlertTriangle size={18} /><div><strong>Konsey için model bağlantısı gerekli</strong><span>Joe bağlantı olmadan sahte analiz üretmez.</span></div><button type="button" onClick={() => onNavigate("saglayicilar")}>Sağlayıcı ekle <ArrowRight size={15} /></button></div>
          ) : (
            <>
              <button type="button" className="gelismis-ac" onClick={() => setAdvancedRoutes((value) => !value)}><ChevronDown size={16} className={advancedRoutes ? "acik" : ""} /> Kişiye özel model dağıtımı <span>isteğe bağlı</span></button>
              {advancedRoutes ? (
                <div className="persona-route-listesi">
                  {selectedPersonas.map((personaId) => {
                    const persona = personas.find((item) => item.id === personaId);
                    return (
                      <label key={personaId}><span><i style={{ background: persona?.accent }} />{persona?.name}</span><select value={routes[personaId] ?? ""} onChange={(event) => setRoutes({ ...routes, [personaId]: event.target.value })}><option value="">Varsayılan bağlantı</option>{connections.map((item) => <option key={item.id} value={item.id}>{item.label} — {item.model}</option>)}</select></label>
                    );
                  })}
                </div>
              ) : null}
            </>
          )}

          {selectedProfile && !selectedProfile.capabilities.local ? (
            <div className="dis-servis-uyarisi"><ShieldCheck size={16} /><span><strong>{selectedProfile.name} dış sağlayıcıdır.</strong> Analizi başlattığında seçili metin bu sağlayıcıya gönderilir; ham dosyanın kendisi gönderilmez.</span></div>
          ) : selectedProfile?.capabilities.local ? (
            <div className="yerel-model-bilgisi"><ShieldCheck size={16} /><span>Seçili varsayılan model yerel çalışıyor.</span></div>
          ) : null}
        </section>

        {error ? <HataKutusu message={error} /> : null}

        <div className="analiz-baslat-cubugu">
          <div><span><UsersRound size={17} /> {selectedPersonas.length} persona</span><span><Quote size={17} /> {analysisText.length.toLocaleString("tr-TR")} karakter</span><span><Scale size={17} /> 4 aşama</span></div>
          <button className="birincil-buton buyuk" disabled={submitting || uploading || (!analysisText.trim() && !artifactId) || !caseId || selectedPersonas.length < 2 || connections.length === 0 || (sourceType === "whatsapp" && !whatsappTarget)}>
            {submitting ? <LoaderCircle size={18} className="donen" /> : <BrainCircuit size={18} />}
            {submitting ? "Konsey sıraya alınıyor…" : "Konseyi çalıştır"}
          </button>
        </div>
      </form>
    </div>
  );
}


function AnalysisRunView({
  session,
  personas,
  resultTab,
  onTab,
  onReset,
  followup,
  onFollowup,
  onAskFollowup,
  submitting,
  error,
}: {
  session: AnalysisSession;
  personas: Persona[];
  resultTab: "sonuc" | "konsey" | "kanit";
  onTab: (tab: "sonuc" | "konsey" | "kanit") => void;
  onReset: () => void;
  followup: string;
  onFollowup: (value: string) => void;
  onAskFollowup: () => void;
  submitting: boolean;
  error: string | null;
}) {
  const phaseIndex = PHASES.findIndex(([id]) => id === session.progress_phase);
  const personaName = (id: string) => personas.find((item) => item.id === id)?.name ?? id;
  const result = session.result;

  return (
    <div>
      <SayfaBasligi
        eyebrow="KONSEY OTURUMU"
        title={session.title}
        description="Bağımsız görüşler, itirazlar ve revizyonlar tamamlandıkça oturum kaydına işlenir."
        actions={<div className="oturum-aksiyonlari"><DurumRozeti durum={session.status} /><button className="ikincil-buton" onClick={onReset}><RotateCcw size={16} /> Yeni analiz</button></div>}
      />

      <section className="konsey-ilerleme">
        {PHASES.map(([id, label], index) => {
          const complete = session.status === "completed" || index < phaseIndex;
          const active = id === session.progress_phase;
          return (
            <div className={`ilerleme-adimi ${complete ? "tamam" : ""} ${active ? "aktif" : ""}`} key={id}>
              <span>{complete ? <Check size={13} /> : active ? <LoaderCircle size={13} className="donen" /> : index + 1}</span>
              <small>{label}</small>
            </div>
          );
        })}
      </section>

      {error ? <HataKutusu message={error} /> : null}
      {session.status === "failed" ? <HataKutusu message={session.error_message || "Konsey çalışması tamamlanamadı."} /> : null}

      {session.status === "queued" || session.status === "running" ? (
        <div className="konsey-canli-layout">
          <section className="panel konsey-canli-panel">
            <div className="konsey-orbit" aria-hidden="true"><div className="orbit-merkez"><BrainCircuit size={27} /></div>{session.selected_personas.slice(0, 8).map((id, index) => { const persona = personas.find((item) => item.id === id); return <span key={id} style={{ "--index": index, "--count": Math.min(session.selected_personas.length, 8), "--persona": persona?.accent ?? "#657" } as React.CSSProperties}>{persona?.name.split(" ").map((part) => part[0]).slice(0, 2).join("")}</span>; })}</div>
            <h2>Konsey düşünüyor</h2>
            <p>{phaseLabel(session.progress_phase)}. Yanıtlar uydurulmadan, gerçek sağlayıcı çağrıları tamamlandıkça kaydediliyor.</p>
            <div className="canli-metrikler"><span><UsersRound size={15} /> {session.selected_personas.length} persona</span><span><MessageSquareMore size={15} /> {session.turns.length} kayıtlı tur</span><span><CircleDotDashed size={15} /> Tam konsey</span></div>
          </section>
          <section className="panel canli-akis-paneli">
            <div className="panel-baslik"><div><span className="eyebrow">CANLI OTURUM KAYDI</span><h2>Konsey konuşmaları</h2></div><span className="canli-yazi"><i /> CANLI</span></div>
            {session.turns.length ? <div className="tur-akisi">{session.turns.slice().reverse().map((turn) => <article key={turn.id}><span style={{ background: personas.find((item) => item.id === turn.persona_id)?.accent }} /><div><strong>{personaName(turn.persona_id)}</strong><small>{turn.phase.replaceAll("_", " ")}</small><p>{turnSummary(turn.payload)}</p></div></article>)}</div> : <div className="tur-bekliyor"><LoaderCircle size={20} className="donen" /> İlk bağımsız görüşler bekleniyor…</div>}
          </section>
        </div>
      ) : null}

      {session.status === "completed" && result ? (
        <>
          <div className="sonuc-sekmeleri" role="tablist">
            <button className={resultTab === "sonuc" ? "aktif" : ""} onClick={() => onTab("sonuc")}><ClipboardCheck size={16} /> Sonuç</button>
            <button className={resultTab === "konsey" ? "aktif" : ""} onClick={() => onTab("konsey")}><UsersRound size={16} /> Konsey kayıtları <span>{session.turns.length}</span></button>
            <button className={resultTab === "kanit" ? "aktif" : ""} onClick={() => onTab("kanit")}><Quote size={16} /> Kanıtlar <span>{result.evidence.length}</span></button>
          </div>

          {resultTab === "sonuc" ? (
            <div className="sonuc-layout">
              <main className="sonuc-ana">
                <section className="panel sentez-karti">
                  <div className="sentez-baslik"><div className="sentez-ikon"><BrainCircuit size={21} /></div><div><span className="eyebrow">ORTAK SENTEZ</span><h2>Konseyin dengeli sonucu</h2></div><span className={`denetim-rozeti ${result.epistemic_audit.valid ? "temiz" : "uyari"}`}>{result.epistemic_audit.valid ? <CheckCircle2 size={14} /> : <AlertTriangle size={14} />}{result.epistemic_audit.valid ? "Denetim temiz" : "İnceleme gerekli"}</span></div>
                  <p className="sentez-ozet">{result.synthesis.executive_summary}</p>
                  <div className="sentez-kapsam"><ShieldCheck size={16} />{result.synthesis.scope_note}</div>
                </section>

                <section className="iddia-listesi">
                  <div className="bolum-basligi"><div><span className="eyebrow">KANITLI ÇIKARIMLAR</span><h2>Olgu, yorum ve belirsizlikler</h2></div><span className="sayac">{result.synthesis.claims.length}</span></div>
                  {result.synthesis.claims.map((claim, index) => <ClaimCard key={`${claim.statement}-${index}`} claim={claim} personaName={personaName} />)}
                </section>

                <section className="panel takip-sorusu">
                  <div><MessageSquareMore size={20} /><div><span className="eyebrow">SOHBET MODU</span><h2>Konseye takip sorusu sor</h2></div></div>
                  <p>Aynı kanıt paketi, önceki sentez ve seçili personellerle bağlantılı yeni bir konsey turu başlatır.</p>
                  <div><textarea value={followup} onChange={(event) => onFollowup(event.target.value)} placeholder="Örn. Jung ile Kristeva'nın ayrıştığı noktayı kanıtlarla yeniden tartışın." /><button className="birincil-buton" onClick={onAskFollowup} disabled={!followup.trim() || submitting}>{submitting ? <LoaderCircle size={16} className="donen" /> : <ArrowRight size={16} />} Konseye sor</button></div>
                </section>
              </main>

              <aside className="sonuc-yan">
                <section className="panel uzlasi-paneli"><span className="eyebrow">UZLAŞILAR</span>{result.synthesis.convergences.length ? <ul>{result.synthesis.convergences.map((item) => <li key={item}><CheckCircle2 size={14} />{item}</li>)}</ul> : <p>Belirgin ortak görüş kaydedilmedi.</p>}</section>
                <section className="panel ayrilik-paneli"><span className="eyebrow">GÖRÜŞ AYRILIKLARI</span>{result.synthesis.disagreements.length ? <ul>{result.synthesis.disagreements.map((item) => <li key={item}><Scale size={14} />{item}</li>)}</ul> : <p>Belirgin görüş ayrılığı kaydedilmedi.</p>}</section>
                <section className="panel eksik-paneli"><span className="eyebrow">EKSİK BAĞLAM</span>{result.synthesis.missing_context.length ? <ul>{result.synthesis.missing_context.map((item) => <li key={item}><CircleDotDashed size={14} />{item}</li>)}</ul> : <p>Ek bağlam isteği yok.</p>}</section>
                <section className="panel denetim-paneli"><span className="eyebrow">EPİSTEMİK DENETİM</span><dl><div><dt>Atıf</dt><dd>{result.epistemic_audit.citation_count}</dd></div><div><dt>Benzersiz kanıt</dt><dd>{result.epistemic_audit.unique_citation_count}</dd></div><div><dt>Geçersiz kimlik</dt><dd>{result.epistemic_audit.invalid_evidence_ids.length}</dd></div><div><dt>Kanıtsız iddia</dt><dd>{result.epistemic_audit.unsupported_claim_indexes.length}</dd></div></dl></section>
              </aside>
            </div>
          ) : null}

          {resultTab === "konsey" ? (
            <div className="konsey-kayitlari">
              {session.turns.map((turn) => {
                const persona = personas.find((item) => item.id === turn.persona_id);
                return <CouncilTurnCard key={turn.id} name={personaName(turn.persona_id)} persona={persona} phase={turn.phase} payload={turn.payload} />;
              })}
            </div>
          ) : null}

          {resultTab === "kanit" ? (
            <div className="kanit-grid">{result.evidence.map((item) => <article key={item.id}><div><code>{item.id}</code><span>{item.sha256.slice(0, 12)}</span></div><p>{item.content}</p></article>)}</div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}


function ClaimCard({ claim, personaName }: { claim: SynthesisClaim; personaName: (id: string) => string }) {
  return (
    <article className={`iddia-karti tur-${claim.kind.replaceAll(" ", "-")}`}>
      <div className="iddia-karti-ust"><span>{claim.kind}</span><span className={`guven guven-${claim.confidence}`}>{claim.confidence} güven</span></div>
      <p>{claim.statement}</p>
      <div className="kanit-cipleri">{claim.evidence_ids.map((id) => <code key={id}>{id}</code>)}</div>
      {(claim.supporting_personas.length || claim.dissenting_personas.length) ? <footer>{claim.supporting_personas.length ? <span><Check size={13} /> Destek: {claim.supporting_personas.map(personaName).join(", ")}</span> : null}{claim.dissenting_personas.length ? <span><Scale size={13} /> İtiraz: {claim.dissenting_personas.map(personaName).join(", ")}</span> : null}</footer> : null}
    </article>
  );
}


function CouncilTurnCard({ name, persona, phase, payload }: { name: string; persona?: Persona; phase: string; payload: Record<string, unknown> }) {
  const thesis = String(payload.revised_thesis ?? payload.thesis ?? payload.executive_summary ?? "Yapılandırılmış konsey kaydı");
  const observations = Array.isArray(payload.observations) ? payload.observations as Array<Record<string, unknown>> : [];
  const challenges = Array.isArray(payload.challenges) ? payload.challenges as Array<Record<string, unknown>> : [];
  return (
    <article className="panel konsey-tur-karti" style={{ "--persona": persona?.accent ?? "#465" } as React.CSSProperties}>
      <header><span className="persona-monogram">{name.split(" ").map((part) => part[0]).slice(0, 2).join("")}</span><div><h2>{name}</h2><span>{phase.replaceAll("_", " ")}</span></div></header>
      <p className="tur-tez">{thesis}</p>
      {observations.length ? <div className="tur-gozlemler">{observations.slice(0, 6).map((item, index) => <div key={index}><strong>{String(item.claim ?? "Gözlem")}</strong><p>{String(item.interpretation ?? "")}</p><span>{Array.isArray(item.evidence_ids) ? item.evidence_ids.join(" · ") : ""}</span></div>)}</div> : null}
      {challenges.length ? <div className="tur-itirazlar">{challenges.slice(0, 6).map((item, index) => <div key={index}><Scale size={14} /><span><strong>{String(item.target_persona_id ?? "Konsey")}</strong>{String(item.issue ?? "")}</span></div>)}</div> : null}
    </article>
  );
}


function phaseLabel(phase: string | null): string {
  const labels: Record<string, string> = {
    "hazırlanıyor": "Kanıt paketi ve model rotaları hazırlanıyor",
    "bağımsız_görüşler": "Her persona diğerlerinden etkilenmeden ilk görüşünü oluşturuyor",
    "çapraz_sorgu": "Personalar birbirlerinin kanıt ve mantık sınırlarını sorguluyor",
    "görüş_revizyonu": "Her persona eleştiriler ışığında görüşünü yeniden yazıyor",
    "ortak_sentez": "Nötr moderatör uzlaşı ve ayrılıkları birlikte sentezliyor",
  };
  return labels[phase ?? ""] ?? "Konsey çalışması ilerliyor";
}


function turnSummary(payload: Record<string, unknown>): string {
  const value = payload.thesis ?? payload.revised_thesis ?? payload.executive_summary;
  if (typeof value === "string") return value.slice(0, 220);
  const challenges = payload.challenges;
  if (Array.isArray(challenges)) return `${challenges.length} somut itiraz kaydedildi.`;
  return "Yapılandırılmış görüş kaydedildi.";
}
