import { FileUp, FolderOpen } from 'lucide-react'
import { useRef, useState } from 'react'
import { api } from '@/lib/api'
import { go } from '@/lib/router'
import { useData } from '@/lib/data'
import { cn } from '@/lib/cn'
import { Button, inputCls } from './ui'

const label = (fs: File[]) => (fs.length > 1 ? `${fs.length} dosya` : fs[0]!.name)

/** Dosya bırakma / yol girme alanı. Büyük Instagram arşivleri için yol girişi önerilir. */
export function Importer({ compact }: { compact?: boolean }) {
  const { reload, toast } = useData()
  const [over, setOver] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [path, setPath] = useState('')
  const input = useRef<HTMLInputElement>(null)

  async function run(label: string, job: () => ReturnType<typeof api.importPath>) {
    setBusy(label)
    try {
      const res = await job()
      await reload()
      if (res.duplicate) toast('Bu kaynak zaten içe aktarılmış.', 'ok')
      else if (res.owner_needed) toast('İçe aktarıldı. Şimdi hesap sahibini doğrula.', 'ok')
      else toast(`İçe aktarıldı: ${res.source.stats.messages ?? 0} mesaj.`, 'ok')
      go('/kaynaklar')
    } catch (e) {
      toast(e instanceof Error ? e.message : 'İçe aktarma başarısız.', 'bad')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-3">
      <div
        onDragOver={(e) => { e.preventDefault(); setOver(true) }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setOver(false)
          const fs = Array.from(e.dataTransfer.files)
          if (fs.length) void run(label(fs), () => api.importFiles(fs))
        }}
        className={cn(
          'flex flex-col items-center justify-center rounded-[18px] border border-dashed text-center transition',
          compact ? 'px-6 py-8' : 'px-6 py-14',
          over ? 'border-accent bg-accent-soft' : 'border-line bg-raised/50',
        )}
      >
        <div className="mb-3 grid size-12 place-items-center rounded-2xl bg-accent-soft text-accent">
          <FileUp className="size-6" />
        </div>
        <p className="text-[15px] font-semibold">Instagram ZIP'ini ya da WhatsApp sohbetini bırak</p>
        <p className="mt-1 max-w-md text-[13px] text-muted">
          Instagram için "Bilgilerini indir → JSON" biçimini seç. Birden çok parça (-1.zip, -2.zip) varsa hepsini birlikte seç. WhatsApp için sohbette "Sohbeti dışa aktar" yeterli.
          Veri yalnızca bu bilgisayarda kalır.
        </p>
        <div className="mt-4 flex gap-2">
          <Button variant="primary" busy={!!busy} onClick={() => input.current?.click()}>Dosya seç</Button>
        </div>
        <input
          ref={input}
          type="file"
          accept=".zip,.txt"
          multiple
          className="hidden"
          onChange={(e) => {
            const fs = Array.from(e.target.files ?? [])
            e.target.value = ''
            if (fs.length) void run(label(fs), () => api.importFiles(fs))
          }}
        />
        {busy && (
          <div className="mt-5 w-full max-w-sm" role="status">
            <div className="h-1.5 overflow-hidden rounded-full bg-sunken">
              <div className="pulse-soft h-full w-2/3 rounded-full bg-accent" />
            </div>
            <p className="mt-2 text-[12px] text-muted">"{busy}" okunuyor… büyük arşivler birkaç dakika sürebilir.</p>
          </div>
        )}
      </div>

      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          const paths = path.split(/\n|;/).map((p) => p.trim()).filter(Boolean)
          if (paths.length) void run(paths.length > 1 ? `${paths.length} parça` : paths[0]!, () => api.importPath(paths))
        }}
      >
        <div className="relative flex-1">
          <FolderOpen className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-faint" />
          <input
            className={cn(inputCls, 'pl-9')}
            placeholder="Ya da yol yaz: /home/ara/instagram-1.zip; /home/ara/instagram-2.zip (büyük arşivler için önerilir)"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            aria-label="Kaynak yolu"
          />
        </div>
        <Button type="submit" disabled={!path.trim() || !!busy}>İçe aktar</Button>
      </form>
    </div>
  )
}
