import type { AnalysisSession } from "./types";


export function analysisSessionPath(session: Pick<AnalysisSession, "id" | "title">): string {
  const slug = session.title
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLocaleLowerCase("tr-TR")
    .replace(/ı/g, "i")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 72) || "analiz";
  return `/analiz/oturum/${slug}--${session.id}`;
}


export function analysisIdFromRoute(value: string): string {
  const candidate = value.includes("--") ? value.split("--").at(-1) || value : value;
  return candidate;
}
