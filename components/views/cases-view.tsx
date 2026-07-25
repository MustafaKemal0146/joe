"use client";

import { FileKey2, FolderKanban, Plus, ShieldCheck, Trash2, UserRoundSearch } from "lucide-react";
import { FormEvent, useState } from "react";
import { joeApi, readableError } from "../../lib/api";
import type { CaseRecord } from "../../lib/types";
import { BosDurum, HataKutusu, KisaTarih, SayfaBasligi } from "../ui";


export function CasesView({
  cases,
  onChanged,
}: {
  cases: CaseRecord[];
  onChanged: () => Promise<void>;
}) {
  const [formOpen, setFormOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [form, setForm] = useState({
    name: "",
    subject_label: "",
    purpose: "",
    authorization_note: "",
  });

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      await joeApi.createCase({
        ...form,
        subject_label: form.subject_label || null,
        purpose: form.purpose || null,
        authorization_note: form.authorization_note || null,
      });
      setForm({ name: "", subject_label: "", purpose: "", authorization_note: "" });
      setFormOpen(false);
      await onChanged();
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setSaving(false);
    }
  }

  async function removeCase(item: CaseRecord) {
    const confirmed = window.confirm(`“${item.name}” vakası ve ona bağlı kanıt/oturum kayıtları kalıcı olarak silinecek. Devam edilsin mi?`);
    if (!confirmed) return;
    setDeletingId(item.id);
    setError(null);
    try {
      await joeApi.deleteCase(item.id);
      await onChanged();
    } catch (caught) {
      setError(readableError(caught));
    } finally {
      setDeletingId(null);
    }
  }

  return (
    <div>
      <SayfaBasligi
        eyebrow="KANIT VE YETKİ DÜZENİ"
        title="Vakalar"
        description="Araştırma ve analizleri amaç, kapsam ve yetki notuyla aynı çalışma dosyasında tut."
        actions={
          <button className="birincil-buton" onClick={() => setFormOpen((value) => !value)}>
            <Plus size={17} /> Yeni vaka
          </button>
        }
      />

      {formOpen ? (
        <form className="panel form-panel" onSubmit={submit}>
          <div className="panel-baslik">
            <div><span className="eyebrow">YENİ ÇALIŞMA DOSYASI</span><h2>Vaka kaydı oluştur</h2></div>
            <ShieldCheck size={21} />
          </div>
          <div className="form-grid iki">
            <label>
              <span>Vaka adı</span>
              <input
                required
                minLength={2}
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                placeholder="Örn. Kullanıcı adı araştırması"
              />
            </label>
            <label>
              <span>Özne etiketi <small>isteğe bağlı</small></span>
              <input
                value={form.subject_label}
                onChange={(event) => setForm({ ...form, subject_label: event.target.value })}
                placeholder="Gerçek isim yerine vaka etiketi kullanabilirsin"
              />
            </label>
            <label>
              <span>Amaç ve kapsam</span>
              <textarea
                value={form.purpose}
                onChange={(event) => setForm({ ...form, purpose: event.target.value })}
                placeholder="Bu çalışma neden yapılıyor, hangi kaynaklar kapsamda?"
              />
            </label>
            <label>
              <span>Yetki / rıza notu</span>
              <textarea
                value={form.authorization_note}
                onChange={(event) => setForm({ ...form, authorization_note: event.target.value })}
                placeholder="Veri sahibinin izni, tarih ve saklama sınırı"
              />
            </label>
          </div>
          {error ? <HataKutusu message={error} /> : null}
          <div className="form-aksiyonlari">
            <button type="button" className="metin-buton" onClick={() => setFormOpen(false)}>Vazgeç</button>
            <button className="birincil-buton" disabled={saving}>{saving ? "Kaydediliyor…" : "Vakayı kaydet"}</button>
          </div>
        </form>
      ) : null}

      {cases.length === 0 ? (
        <BosDurum
          icon={<FolderKanban size={25} />}
          title="Henüz vaka yok"
          description="Bir vaka açtığında OSINT bulguları, yüklenen dosyalar ve konsey analizleri aynı kayıt altında izlenebilir."
          action={<button className="ikincil-buton" onClick={() => setFormOpen(true)}><Plus size={16} /> İlk vakayı oluştur</button>}
        />
      ) : (
        <div className="vaka-grid">
          {cases.map((item) => (
            <article className="vaka-karti" key={item.id}>
              <div className="vaka-karti-ust">
                <div className="vaka-ikon"><FolderKanban size={20} /></div>
                <div className="flex items-center gap-2"><span className="durum-rozeti durum-active">Etkin</span><button type="button" className="ikon-buton tehlike" onClick={() => void removeCase(item)} disabled={deletingId === item.id} aria-label={`${item.name} vakasını sil`}><Trash2 size={16} /></button></div>
              </div>
              <a href={`/vakalar/${item.id}`} style={{ textDecoration: "none", color: "inherit", display: "block" }}><h2>{item.name}</h2><div className="vaka-ozne"><UserRoundSearch size={15} /> {item.subject_label || "Özne etiketi girilmedi"}</div><p>{item.purpose || "Amaç ve kapsam notu eklenmedi."}</p><div className="vaka-yetki"><FileKey2 size={15} /><span>{item.authorization_note ? "Yetki notu kayıtlı" : "Yetki notu bekliyor"}</span></div><footer><span>Güncelleme</span><KisaTarih value={item.updated_at} /></footer></a>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
