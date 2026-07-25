import type {
  AnalysisSession,
  ArtifactRecord,
  CaseRecord,
  CorpusRecord,
  CorpusSearchResult,
  DashboardSummary,
  OsintPlan,
  OsintRun,
  Persona,
  ProviderConnection,
  ProviderProfile,
  ImportBrowse,
  ImportStatus,
} from "./types";

export const API_BASE =
  process.env.NEXT_PUBLIC_JOE_API_URL ?? "http://localhost:8000/api/v1";

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      ...(init?.body instanceof FormData ? {} : { "Content-Type": "application/json" }),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    let message = `İstek başarısız (${response.status})`;
    try {
      const payload = (await response.json()) as { detail?: string | Array<{ msg: string }> };
      if (typeof payload.detail === "string") message = payload.detail;
      else if (Array.isArray(payload.detail)) message = payload.detail.map((item) => item.msg).join(" · ");
    } catch {
      // Sunucu JSON döndürmediyse durum mesajı yeterlidir.
    }
    throw new ApiError(message, response.status);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const joeApi = {
  health: () => request<{ status: string }>("/health"),
  summary: () => request<DashboardSummary>("/summary"),
  personas: () => request<Persona[]>("/personas"),
  providerProfiles: () => request<ProviderProfile[]>("/providers/profiles"),
  providerConnections: () => request<ProviderConnection[]>("/providers/connections"),
  createProviderConnection: (body: Record<string, unknown>) =>
    request<ProviderConnection>("/providers/connections", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  deleteProviderConnection: (id: string) =>
    request<void>(`/providers/connections/${id}`, { method: "DELETE" }),
  testProviderConnection: (id: string) =>
    request<{ status: string; model: string }>(`/providers/connections/${id}/test`, {
      method: "POST",
    }),
  cases: () => request<CaseRecord[]>("/cases"),
  createCase: (body: Record<string, unknown>) =>
    request<CaseRecord>("/cases", { method: "POST", body: JSON.stringify(body) }),
  analyses: () => request<AnalysisSession[]>("/analyses"),
  analysis: (id: string) => request<AnalysisSession>(`/analyses/${id}`),
  createAnalysis: (body: Record<string, unknown>) =>
    request<AnalysisSession>("/analyses", { method: "POST", body: JSON.stringify(body) }),
  cancelAnalysis: (id: string) =>
    request<AnalysisSession>(`/analyses/${id}/cancel`, { method: "POST" }),
  osintPlan: (body: { query: string; query_type: "username" | "full_name" }) =>
    request<OsintPlan>("/osint/plan", { method: "POST", body: JSON.stringify(body) }),
  osintRuns: () => request<OsintRun[]>("/osint/runs"),
  osintRun: (id: string) => request<OsintRun>(`/osint/runs/${id}`),
  createOsintRun: (body: Record<string, unknown>) =>
    request<OsintRun>("/osint/runs", { method: "POST", body: JSON.stringify(body) }),
  uploadArtifact: (file: File, caseId?: string) => {
    const body = new FormData();
    body.append("file", file);
    if (caseId) body.append("case_id", caseId);
    return request<ArtifactRecord>("/artifacts", { method: "POST", body });
  },
  importStatus: () => request<ImportStatus>("/imports/status"),
  browseImports: (path = ".") =>
    request<ImportBrowse>(`/imports/browse?path=${encodeURIComponent(path)}`),
  corpora: () => request<CorpusRecord[]>("/corpora"),
  corpus: (id: string) => request<CorpusRecord>(`/corpora/${id}`),
  createCorpus: (body: { name: string; relative_path: string; case_id: string }) =>
    request<CorpusRecord>("/corpora", { method: "POST", body: JSON.stringify(body) }),
  searchCorpus: (id: string, body: { query: string; max_results?: number }) =>
    request<CorpusSearchResult>(`/corpora/${id}/search`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
};

export function readableError(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "Beklenmeyen bir hata oluştu.";
}
