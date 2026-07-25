export type Gorunum = "genel" | "osint" | "analiz" | "vakalar" | "saglayicilar";

export interface DashboardSummary {
  case_count: number;
  analysis_count: number;
  osint_run_count: number;
  provider_connection_count: number;
  active_jobs: number;
}

export interface Persona {
  id: string;
  name: string;
  title: string;
  group: string;
  accent: string;
  summary: string;
  concepts: string[];
  analysis_questions: string[];
}

export interface ProviderCapabilities {
  structured_output: boolean;
  tools: boolean;
  vision: boolean;
  model_listing: boolean;
  local: boolean;
}

export interface ProviderProfile {
  id: string;
  name: string;
  protocol: string;
  default_base_url: string | null;
  requires_api_key: boolean;
  api_key_label: string;
  model_hint: string | null;
  docs_url: string | null;
  capabilities: ProviderCapabilities;
  aliases: string[];
  notes: string | null;
}

export interface ProviderConnection {
  id: string;
  provider_id: string;
  label: string;
  model: string;
  base_url: string | null;
  enabled: boolean;
  has_api_key: boolean;
  created_at: string;
  updated_at: string;
}

export interface CaseRecord {
  id: string;
  name: string;
  subject_label: string | null;
  purpose: string | null;
  authorization_note: string | null;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface CaseWorkspace {
  case: CaseRecord;
  analyses: AnalysisSession[];
  osint_runs: OsintRun[];
  corpora: CorpusRecord[];
  imports: Array<{
    id: string;
    case_id: string;
    source_type: string;
    source_root: string;
    status: string;
    imported_profiles: number;
    imported_messages: number;
    import_summary: Record<string, unknown>;
    error_code: string | null;
    error_message: string | null;
    created_at: string;
    updated_at: string;
  }>;
}

export interface CouncilTurn {
  id: string;
  phase: string;
  persona_id: string;
  provider_connection_id: string | null;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface AnalysisSession {
  id: string;
  case_id: string | null;
  title: string;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  source_type: string;
  selected_personas: string[];
  provider_routes: Record<string, string>;
  default_provider_connection_id: string | null;
  result: AnalysisResult | null;
  error_code: string | null;
  error_message: string | null;
  progress_phase: string | null;
  created_at: string;
  updated_at: string;
  turns: CouncilTurn[];
}

export interface SynthesisClaim {
  statement: string;
  kind: "gözlem" | "kuramsal yorum" | "karşı hipotez" | "belirsizlik";
  evidence_ids: string[];
  supporting_personas: string[];
  dissenting_personas: string[];
  confidence: "düşük" | "orta" | "yüksek";
}

export interface AnalysisResult {
  version: string;
  protocol: "tam_konsey";
  evidence: Array<{ id: string; content: string; sha256: string }>;
  independent_analyses: Record<string, Record<string, unknown>>;
  challenges: Record<string, Record<string, unknown>>;
  final_persona_outputs: Record<string, Record<string, unknown>>;
  synthesis: {
    executive_summary: string;
    claims: SynthesisClaim[];
    convergences: string[];
    disagreements: string[];
    missing_context: string[];
    scope_note: string;
  };
  epistemic_audit: {
    valid: boolean;
    invalid_evidence_ids: string[];
    unsupported_claim_indexes: number[];
    diagnostic_language_flags: string[];
    citation_count: number;
    unique_citation_count: number;
  };
  run_metadata: Record<string, unknown>;
}

export interface OsintPlan {
  query: string;
  query_type: "username" | "full_name";
  searches: Array<{
    engine: string;
    query: string;
    url: string;
    classification: string;
  }>;
  direct_profile_leads: Array<{ label: string; url: string }>;
  notice: string;
}

export interface OsintFinding {
  connector: string;
  site: string;
  username: string;
  profile_url: string;
  site_url: string;
  http_status: string | null;
  response_time_seconds: string | null;
  classification: string;
  identity_status: string;
  sources: string[];
}

export interface OsintRun {
  id: string;
  case_id: string | null;
  query: string;
  query_type: "username" | "full_name";
  connectors: string[];
  status: "queued" | "running" | "completed" | "failed";
  result: null | {
    manual_plan: OsintPlan;
    connector_results: Record<
      string,
      { findings: OsintFinding[]; finding_count: number; notice: string }
    >;
    findings: OsintFinding[];
    coverage: {
      requested_connectors: string[];
      completed_connectors: string[];
      raw_finding_count: number;
      unique_finding_count: number;
      partial: boolean;
    };
    errors: Array<{ connector: string; code: string; message: string }>;
  };
  error_code: string | null;
  error_message: string | null;
  created_at: string;
  updated_at: string;
}

export interface ArtifactRecord {
  id: string;
  original_name: string;
  media_type: string;
  sha256: string;
  size_bytes: number;
  extractor: string;
  extracted_text: string | null;
  artifact_metadata: Record<string, unknown>;
  created_at: string;
}

export interface ImportStatus {
  configured: boolean;
  available: boolean;
  root_label: string;
}

export interface ImportEntry {
  name: string;
  relative_path: string;
  kind: "dizin" | "dosya";
  size_bytes: number | null;
}

export interface ImportBrowse {
  path: string;
  parent_path: string | null;
  entries: ImportEntry[];
}

export interface CorpusRecord {
  id: string;
  case_id: string | null;
  name: string;
  relative_path: string;
  status: "queued" | "running" | "completed" | "failed";
  file_count: number;
  indexed_file_count: number;
  total_bytes: number;
  error_count: number;
  scan_summary: Record<string, unknown>;
  error_code: string | null;
  error_message: string | null;
  created_at: string;
  updated_at: string;
}

export interface CorpusSearchResult {
  corpus_id: string;
  query: string;
  hits: Array<{
    document_id: string;
    relative_path: string;
    extractor: string;
    score: number;
    excerpt: string;
  }>;
  analysis_text: string;
  truncated: boolean;
}
