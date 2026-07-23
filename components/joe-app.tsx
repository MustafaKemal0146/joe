"use client";

import {
  Binoculars,
  BrainCircuit,
  ChevronRight,
  CircleGauge,
  DatabaseZap,
  FolderKanban,
  Menu,
  PlugZap,
  ShieldCheck,
  X,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { joeApi } from "../lib/api";
import type {
  CaseRecord,
  DashboardSummary,
  Gorunum,
  Persona,
  ProviderConnection,
  ProviderProfile,
} from "../lib/types";
import { AnalysisView } from "./views/analysis-view";
import { CasesView } from "./views/cases-view";
import { DashboardView } from "./views/dashboard-view";
import { OsintView } from "./views/osint-view";
import { ProvidersView } from "./views/providers-view";


const NAV_ITEMS: Array<{
  id: Gorunum;
  label: string;
  helper: string;
  icon: typeof CircleGauge;
}> = [
  { id: "genel", label: "Vaka Masası", helper: "Aktif çalışmalar", icon: CircleGauge },
  { id: "osint", label: "OSINT", helper: "Keşfet", icon: Binoculars },
  { id: "analiz", label: "Analiz", helper: "Konsey", icon: BrainCircuit },
  { id: "vakalar", label: "Vakalar", helper: "Kanıt düzeni", icon: FolderKanban },
  { id: "saglayicilar", label: "Sağlayıcılar", helper: "Model bağlantıları", icon: PlugZap },
];


export function JoeApp() {
  const [gorunum, setGorunum] = useState<Gorunum>("genel");
  const [menuAcik, setMenuAcik] = useState(false);
  const [apiSaglikli, setApiSaglikli] = useState(false);
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [profiles, setProfiles] = useState<ProviderProfile[]>([]);
  const [connections, setConnections] = useState<ProviderConnection[]>([]);
  const [cases, setCases] = useState<CaseRecord[]>([]);
  const [loading, setLoading] = useState(true);

  const refreshGlobal = useCallback(async () => {
    try {
      const [health, summaryResult, personaResult, profileResult, connectionResult, caseResult] =
        await Promise.all([
          joeApi.health(),
          joeApi.summary(),
          joeApi.personas(),
          joeApi.providerProfiles(),
          joeApi.providerConnections(),
          joeApi.cases(),
        ]);
      setApiSaglikli(health.status === "sağlıklı");
      setSummary(summaryResult);
      setPersonas(personaResult);
      setProfiles(profileResult);
      setConnections(connectionResult);
      setCases(caseResult);
    } catch {
      setApiSaglikli(false);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => void refreshGlobal(), 0);
    return () => window.clearTimeout(timer);
  }, [refreshGlobal]);

  const go = (next: Gorunum) => {
    setGorunum(next);
    setMenuAcik(false);
  };

  const active = NAV_ITEMS.find((item) => item.id === gorunum) ?? NAV_ITEMS[0];

  return (
    <div className="joe-kabuk">
      <aside className={`yan-panel ${menuAcik ? "mobil-acik" : ""}`}>
        <div className="marka">
          <strong className="joe-yazi">joe</strong>
          <button className="ikon-buton mobil-kapat" onClick={() => setMenuAcik(false)} aria-label="Menüyü kapat">
            <X size={19} />
          </button>
        </div>

        <nav aria-label="Ana menü" className="ana-menu">
          <span className="menu-baslik">Çalışma alanı</span>
          {NAV_ITEMS.map((item) => {
            const Icon = item.icon;
            const selected = item.id === gorunum;
            return (
              <button
                key={item.id}
                className={`menu-ogesi ${selected ? "aktif" : ""}`}
                onClick={() => go(item.id)}
                aria-current={selected ? "page" : undefined}
              >
                <Icon size={19} strokeWidth={1.8} aria-hidden="true" />
                <span>
                  <strong>{item.label}</strong>
                  <small>{item.helper}</small>
                </span>
                {selected ? <ChevronRight size={15} aria-hidden="true" /> : null}
              </button>
            );
          })}
        </nav>

        <div className="yan-panel-alt">
          <div className="yerel-kart">
            <div className="yerel-kart-baslik">
              <ShieldCheck size={17} />
              <strong>Yerel çalışma</strong>
            </div>
            <p>Vaka ve kanıtlar bu cihazdaki Joe servislerinde tutulur.</p>
            <div className="servis-satiri">
              <span className={`durum-noktasi ${apiSaglikli ? "cevrimici" : "cevrimdisi"}`} />
              {apiSaglikli ? "API bağlı" : "API bekleniyor"}
            </div>
          </div>
          <div className="surum-satiri">
            <span>Joe çekirdeği</span>
            <code>v0.1</code>
          </div>
        </div>
      </aside>

      {menuAcik ? <button className="mobil-perde" onClick={() => setMenuAcik(false)} aria-label="Menüyü kapat" /> : null}

      <main className="ana-alan">
        <header className="ust-cubuk">
          <div className="ust-sol">
            <button className="ikon-buton mobil-menu" onClick={() => setMenuAcik(true)} aria-label="Menüyü aç">
              <Menu size={20} />
            </button>
            <div className="sayfa-konumu">
              <span>Joe</span>
              <ChevronRight size={13} />
              <strong>{active.label}</strong>
            </div>
          </div>
          <div className="ust-sag">
            <div className="gizlilik-etiketi">
              <DatabaseZap size={15} />
              Yerel veri
            </div>
            <div className={`api-etiketi ${apiSaglikli ? "bagli" : "kesik"}`}>
              <span />
              {apiSaglikli ? "Sistem hazır" : "Servis bağlantısı yok"}
            </div>
          </div>
        </header>

        <div className="sayfa-icerigi">
          {gorunum === "genel" ? (
            <DashboardView
              summary={summary}
              cases={cases}
              loading={loading}
              apiHealthy={apiSaglikli}
              onNavigate={go}
            />
          ) : null}
          {gorunum === "osint" ? <OsintView cases={cases} onChanged={refreshGlobal} /> : null}
          {gorunum === "analiz" ? (
            <AnalysisView
              personas={personas}
              connections={connections}
              profiles={profiles}
              cases={cases}
              onNavigate={go}
            />
          ) : null}
          {gorunum === "vakalar" ? (
            <CasesView
              cases={cases}
              onChanged={async () => {
                await refreshGlobal();
              }}
            />
          ) : null}
          {gorunum === "saglayicilar" ? (
            <ProvidersView
              profiles={profiles}
              connections={connections}
              onChanged={async () => {
                await refreshGlobal();
              }}
            />
          ) : null}
        </div>
      </main>

      <nav className="mobil-alt-menu" aria-label="Mobil ana menü">
        {NAV_ITEMS.slice(0, 4).map((item) => {
          const Icon = item.icon;
          return (
            <button key={item.id} className={gorunum === item.id ? "aktif" : ""} onClick={() => go(item.id)}>
              <Icon size={19} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>
    </div>
  );
}
