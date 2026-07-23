"use client";

import {
  Check,
  Cloud,
  Cpu,
  ExternalLink,
  KeyRound,
  Link2,
  LoaderCircle,
  PlugZap,
  Plus,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import { FormEvent, useMemo, useState } from "react";
import { joeApi, readableError } from "../../lib/api";
import type { ProviderConnection, ProviderProfile } from "../../lib/types";
import { BosDurum, HataKutusu, KisaTarih, SayfaBasligi } from "../ui";


export function ProvidersView({
  profiles,
  connections,
  onChanged,
}: {
  profiles: ProviderProfile[];
  connections: ProviderConnection[];
  onChanged: () => Promise<void>;
}) {
  const [formOpen, setFormOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [testStates, setTestStates] = useState<Record<string, { ok: boolean; message: string }>>({});
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState({
    provider_id: profiles[0]?.id ?? "openai",
    label: "",
    model: "",
    base_url: "",
    api_key: "",
    deployment: "",
    api_version: "",
  });

  const selected = useMemo(
    () => profiles.find((item) => item.id === form.provider_id),
    [profiles, form.provider_id],
  );

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      await joeApi.createProviderConnection({
        provider_id: form.provider_id,
        label: form.label,
        model: form.model,
        base_url: form.base_url || null,
        api_key: form.api_key || null,
        extra_config: {
          ...(form.deployment ? { deployment: form.deployment } : {}),
          ...(form.api_version ? { api_version: form.api_version } : {}),
        },
      });
      setForm({
        provider_id: profiles[0]?.id ?? "openai",
        label: "",
        model: "",
        base_url: "",
        api_key: "",
        deployment: "",
        api_version: "",
      });
      setFormOpen(false);
      await onChanged();
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setSaving(false);
    }
  }

  async function testConnection(connection: ProviderConnection) {
    setTesting(connection.id);
    setTestStates((current) => ({ ...current, [connection.id]: { ok: false, message: "Bağlantı deneniyor…" } }));
    try {
      const result = await joeApi.testProviderConnection(connection.id);
      setTestStates((current) => ({
        ...current,
        [connection.id]: { ok: true, message: `${result.model} yanıt verdi` },
      }));
    } catch (caught) {
      setTestStates((current) => ({
        ...current,
        [connection.id]: { ok: false, message: readableError(caught) },
      }));
    } finally {
      setTesting(null);
    }
  }

  async function removeConnection(connection: ProviderConnection) {
    if (!window.confirm(`“${connection.label}” bağlantısını silmek istediğine emin misin?`)) return;
    setError(null);
    try {
      await joeApi.deleteProviderConnection(connection.id);
      await onChanged();
    } catch (caught) {
      setError(readableError(caught));
    }
  }

  return (
    <div>
      <SayfaBasligi
        eyebrow="HERMES ESİNTİLİ DİNAMİK KAYIT"
        title="AI Sağlayıcıları"
        description="Bir protokole kilitlenmeden bulut, yerel ve OpenAI uyumlu modelleri aynı konsey motoruna bağla."
        actions={
          <button className="birincil-buton" onClick={() => setFormOpen((value) => !value)}>
            <Plus size={17} /> Bağlantı ekle
          </button>
        }
      />

      <div className="bilgi-seridi">
        <ShieldCheck size={18} />
        <div>
          <strong>Anahtarlar tarayıcıya geri gönderilmez.</strong>
          <span>API anahtarları yerel Joe kasasında şifrelenir. Dış sağlayıcı seçilirse yalnızca o analizin seçilmiş içeriği dışarı çıkar.</span>
        </div>
      </div>

      {formOpen ? (
        <form className="panel form-panel saglayici-formu" onSubmit={submit}>
          <div className="panel-baslik">
            <div><span className="eyebrow">YENİ MODEL BAĞLANTISI</span><h2>Sağlayıcıyı kaydet</h2></div>
            <button type="button" className="ikon-buton" onClick={() => setFormOpen(false)} aria-label="Formu kapat"><X size={18} /></button>
          </div>
          <div className="form-grid iki">
            <label>
              <span>Sağlayıcı profili</span>
              <select
                value={form.provider_id}
                onChange={(event) => setForm({ ...form, provider_id: event.target.value })}
              >
                {profiles.map((profile) => (
                  <option key={profile.id} value={profile.id}>{profile.name}</option>
                ))}
              </select>
              {selected ? <small>{selected.capabilities.local ? "Yerel çalışma" : "Dış servis"} · {selected.protocol}</small> : null}
            </label>
            <label>
              <span>Bağlantı etiketi</span>
              <input required value={form.label} onChange={(event) => setForm({ ...form, label: event.target.value })} placeholder="Örn. Yerel Llama — derin analiz" />
            </label>
            <label>
              <span>Model kimliği</span>
              <input required value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })} placeholder={selected?.model_hint ?? "Sağlayıcının model kimliği"} />
            </label>
            <label>
              <span>{selected?.api_key_label ?? "API anahtarı"}</span>
              <div className="girdi-ikonlu"><KeyRound size={16} /><input type="password" required={selected?.requires_api_key} value={form.api_key} onChange={(event) => setForm({ ...form, api_key: event.target.value })} placeholder={selected?.requires_api_key ? "••••••••••••••••" : "İsteğe bağlı"} autoComplete="new-password" /></div>
            </label>
            <label className="tam-satir">
              <span>Özel temel URL <small>{selected?.default_base_url ? "boş bırakırsan profil adresi kullanılır" : "gerekli"}</small></span>
              <div className="girdi-ikonlu"><Link2 size={16} /><input value={form.base_url} onChange={(event) => setForm({ ...form, base_url: event.target.value })} placeholder={selected?.default_base_url ?? "http://host.docker.internal:.../v1"} /></div>
            </label>
            {selected?.id === "azure-openai" ? (
              <>
                <label><span>Azure deployment</span><input value={form.deployment} onChange={(event) => setForm({ ...form, deployment: event.target.value })} /></label>
                <label><span>API sürümü</span><input value={form.api_version} onChange={(event) => setForm({ ...form, api_version: event.target.value })} placeholder="2024-10-21" /></label>
              </>
            ) : null}
          </div>
          {selected?.notes ? <p className="form-notu">{selected.notes}</p> : null}
          {error ? <HataKutusu message={error} /> : null}
          <div className="form-aksiyonlari">
            {selected?.docs_url ? <a className="dokuman-linki" href={selected.docs_url} target="_blank" rel="noreferrer">Sağlayıcı belgeleri <ExternalLink size={14} /></a> : <span />}
            <button className="birincil-buton" disabled={saving}>{saving ? "Şifreleniyor…" : "Bağlantıyı kaydet"}</button>
          </div>
        </form>
      ) : null}

      {error && !formOpen ? <HataKutusu message={error} /> : null}

      <section className="bolum-basligi">
        <div><span className="eyebrow">KAYITLI BAĞLANTILAR</span><h2>Konseyin kullanabileceği modeller</h2></div>
        <span className="sayac">{connections.length}</span>
      </section>

      {connections.length === 0 ? (
        <BosDurum
          icon={<PlugZap size={25} />}
          title="Henüz AI bağlantısı yok"
          description="Ollama gibi yerel bir model veya istediğin dış sağlayıcıyı eklemeden konsey sahte yanıt üretmez."
          action={<button className="ikincil-buton" onClick={() => setFormOpen(true)}><Plus size={16} /> İlk bağlantıyı ekle</button>}
        />
      ) : (
        <div className="saglayici-baglanti-grid">
          {connections.map((connection) => {
            const profile = profiles.find((item) => item.id === connection.provider_id);
            const state = testStates[connection.id];
            const LocalIcon = profile?.capabilities.local ? Cpu : Cloud;
            return (
              <article className="saglayici-karti" key={connection.id}>
                <div className="saglayici-karti-ust">
                  <div className={`saglayici-logo ${profile?.capabilities.local ? "yerel" : "bulut"}`}><LocalIcon size={20} /></div>
                  <div><h3>{connection.label}</h3><span>{profile?.name ?? connection.provider_id}</span></div>
                  <button className="ikon-buton tehlike" onClick={() => void removeConnection(connection)} aria-label="Bağlantıyı sil"><Trash2 size={16} /></button>
                </div>
                <div className="model-satiri"><code>{connection.model}</code><span>{profile?.protocol}</span></div>
                <dl>
                  <div><dt>Anahtar</dt><dd>{connection.has_api_key ? "Şifreli kayıtlı" : "Gerekmiyor"}</dd></div>
                  <div><dt>Tür</dt><dd>{profile?.capabilities.local ? "Yerel" : "Dış servis"}</dd></div>
                  <div><dt>Eklenme</dt><dd><KisaTarih value={connection.created_at} /></dd></div>
                </dl>
                {state ? (
                  <div className={`baglanti-sonucu ${state.ok ? "basarili" : "hatali"}`}>
                    {state.ok ? <Check size={14} /> : testing === connection.id ? <LoaderCircle size={14} className="donen" /> : <X size={14} />}
                    {state.message}
                  </div>
                ) : null}
                <button className="kart-alt-butonu" onClick={() => void testConnection(connection)} disabled={testing === connection.id}>
                  {testing === connection.id ? <LoaderCircle size={15} className="donen" /> : <PlugZap size={15} />}
                  Bağlantıyı gerçek istekle dene
                </button>
              </article>
            );
          })}
        </div>
      )}

      <section className="profil-katalog">
        <div className="bolum-basligi"><div><span className="eyebrow">DİNAMİK PROFİL KATALOĞU</span><h2>Hazır sağlayıcı protokolleri</h2></div><span className="sayac">{profiles.length}</span></div>
        <div className="profil-cipleri">
          {profiles.map((profile) => (
            <span key={profile.id}><i className={profile.capabilities.local ? "yerel" : "bulut"} />{profile.name}<small>{profile.protocol}</small></span>
          ))}
        </div>
      </section>
    </div>
  );
}
