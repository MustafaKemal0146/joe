export type Platform = 'instagram' | 'whatsapp'

export interface Source {
  id: number
  platform: Platform
  label: string
  owner_name: string
  owner_handle: string
  owner_confirmed: boolean
  imported_at: string
  stats: Record<string, number>
  warnings: string[]
  candidates: string[]
}

export interface Identity { id: number; value: string; norm: string }
export interface Person {
  id: number
  name: string
  note: string
  is_self: boolean
  created_at: string
  identities: Identity[]
}

export interface Count { key: string; count: number }
export interface Pair { subject: number; partner: number }
export interface PairF { subject: number; partner: number }
export interface Latency { median: number; p90: number; n: number }

export interface Partner {
  name: string
  platform: Platform
  conv_id: number
  source_id: number
  total: number
  messages: Pair
  subject_share: number
  sessions: number
  starts: Pair
  ends: Pair
  double_texts: Pair
  latency: { subject: Latency; partner: Latency }
  avg_words: PairF
  unsent: Pair
  media: Pair
  first: string
  last: string
  longest_silence_days: number
  silence_from: string
  monthly: { month: string; subject: number; partner: number; latency_subject: number }[]
}

export interface GroupStat {
  title: string; conv_id: number; source_id: number; platform: Platform; members: number; total: number
  subject_msgs: number; subject_share: number; rank: number; first: string; last: string
  hours: number[]; monthly: { month: string; count: number }[] | null
  replies_to: Count[] | null; replied_by: Count[] | null; top: Count[] | null
}

export interface Period { from: string; to: string; kind: 'surge' | 'lull'; count: number; baseline: number }

export interface SearchStat { key: string; count: number; first: string; last: string }

export interface Activity {
  counts: Count[]
  searched: SearchStat[]
  keywords: SearchStat[]
  liked: Count[]
  commented: Count[]
  story: Count[]
  saved: Count[]
  follow: {
    followers: number; following: number; mutual: number; not_following_back: number; fans: number
    unfollowed: number; pending: number; close_friends: number; blocked: number; restricted: number
  }
  follow_monthly: { month: string; following: number; followers: number; unfollowed: number }[]
  logins: { total: number; distinct_ips: number; devices: Count[]; hours: number[]; first: string; last: string }
  media: { with_meta: number; with_gps: number; devices: Count[] | null; places: { lat: number; lon: number; count: number; first: string; last: string }[] | null; first: string; last: string }
  posts: { posts: number; stories: number; reels: number; monthly: { month: string; count: number }[] | null; hours: number[]; first: string; last: string }
  interests: string[] | null
  profile_changes: { ts: string; what: string; from: string; to: string }[] | null
}

export interface Profile {
  subject: string
  messages: number
  first: string
  last: string
  active_days: number
  hours: number[]
  weekday: number[]
  heat: number[][]
  monthly: { month: string; count: number }[] | null
  night_ratio: number
  sleep_start: number
  sleep_known: boolean
  language: {
    text_messages: number; avg_words: number; avg_chars: number; question_ratio: number; exclaim_ratio: number
    link_ratio: number; caps_ratio: number; ellipsis_ratio: number; emoji_per_msg: number; richness: number
    top_words: Count[] | null; top_emoji: Count[] | null; kinds: Count[] | null
  }
  partners: Partner[] | null
  groups: GroupStat[] | null
  periods: Period[] | null
  activity?: Activity
  notes: string[]
  latency: Latency
}

export interface TimelineItem { ts: string; kind: string; title: string; detail?: string; source?: string; weight: number }

export interface GraphNode { id: string; label: string; person_id?: number; is_owner: boolean; strength: number }
export interface GraphEdge { a: string; b: string; strength: number; breakdown: Record<string, number>; out: number; in: number }
export interface HiddenNode { id: string; label: string; hidden_at?: string }
export interface Graph { nodes: GraphNode[]; edges: GraphEdge[]; total_nodes: number; hidden?: HiddenNode[] }
export interface EgoNeighbor extends GraphNode { breakdown: Record<string, number>; relations: string[]; circle: 'yakin' | 'genis' }
export interface Ego { center: GraphNode; neighbors: EgoNeighbor[]; edges: GraphEdge[]; total_neighbors: number; own_data: boolean }
export interface LinkSuggestion { name: string; handle: string; score: number }

export interface Dimension { key: string; label: string; score: number; detail: string }
export interface Comparison {
  a: string; b: string
  dimensions: Dimension[] | null
  overall: number
  direct?: Partner & { platforms: string[] }
  shared_words: string[]
  shared_people: string[]
  missing: string[]
  caveat: string
}

export interface Contact {
  name: string; conv_id: number; is_group: boolean; count: number; first: string; last: string; person_id?: number
}

export interface ImportSummary { conversations: number; messages: number; first: string; last: string; warnings: string[]; titles: string[]; participants: string[] }
export interface ImportResult { source: Source; duplicate: boolean; owner_needed: boolean; person?: Person; partners?: Person[]; summary: ImportSummary }

export interface SearchHit {
  id: number; conv_id: number; source_id: number; title: string; snippet: string
  sender: string; ts: string; kind: string; text: string; score?: number
}

export interface EmbedStatus { configured: boolean; running?: boolean; error?: string; total?: number; done?: number; model?: string; url?: string }

export interface ProviderStatus {
  id: string; name: string; base_url: string; default_model: string
  needs_key: boolean; key_url?: string; editable?: boolean; has_key: boolean; active: boolean
}
export interface Settings {
  providers: ProviderStatus[]
  active_provider: string
  active_model: string
  budget_usd: number
  strong_model?: string
  strong_model_effective?: string
  embed: { url: string; model: string; has_key: boolean; auto: boolean }
  models: Record<string, string[] | null>
  prices: Record<string, { in: number; out: number }>
  ready: boolean
}

export interface AppState {
  sources: number
  people: number
  provider_ready: boolean
  provider: string
  model: string
  needs_owner: number[]
}

export interface Spend {
  total_usd: number
  calls: number
  cached_calls: number
  prompt_tokens: number
  completion_tokens: number
  sessions: { session: string; usd: number; calls: number; cached_calls: number; last: string }[]
}

export interface Risk {
  concern: number
  acute: number
  messages: { id: number; conv_id: number; ts: string; level: 'concern' | 'acute'; categories: string[] }[]
  note: string
}

export interface PersonaInfo { id: string; name: string; school: string; framework: string; focus: string[]; limits: string[]; source: string }

export interface PlanEstimate {
  known: boolean; model: string; calls: number; prompt_tokens: number; output_tokens: number
  low_usd: number; high_usd: number; note?: string
}
export interface CouncilPlan {
  kind: 'single' | 'pair' | 'ask'
  persona_ids: string[]
  estimate: PlanEstimate
  cap_usd: number
  preview: string
  mask_report: Record<string, number> | null
  pack_tokens: number
  items: { id: string; title: string; kind: string; why: string; messages: number; tokens: number }[]
  skipped: string[]
  model: string
  strong_model?: string
  depth?: string
  coverage?: { items: number; messages: number; total_messages: number; kinds: Record<string, number>; months_covered: number; months_total: number }
  provider: string
  ready: boolean
  allowed: boolean
  reason?: string
  approval_id?: string
  expires_at?: string
}

export interface EvidenceMsg { id: number; sender: string; ts: string; kind: string; text: string }
export interface EvidenceItem {
  id: string; kind: string; title: string; why: string; partner: string; conv_id: number
  from: string; to: string; messages: EvidenceMsg[]
}

export interface CouncilSessionRow {
  id: string; kind: 'single' | 'pair' | 'ask'; subject_ids: number[]; persona_ids: string[]
  provider: string; model: string; status: string; pack_hash: string; question?: string; error?: string
  created_at: string; updated_at: string; spent_usd?: number; running?: boolean
  pack?: { subject: string; items: EvidenceItem[]; skipped: string[]; metrics: string }
}

export interface AuditViolation { rule: string; claim: number; message: string; match?: string }
export interface CouncilOutputRow {
  id: number; stage: 'persona' | 'moderator' | 'synthesis' | 'answer' | 'verify' | 'digest'; persona_id: string
  status: 'running' | 'pending' | 'done' | 'failed' | 'skipped'
  output?: unknown; audit?: { violations: AuditViolation[] | null; cited: string[] | null }
  prompt_tokens: number; completion_tokens: number; cost_usd: number; from_cache: boolean; attempts: number; error?: string
}
export interface CouncilDetail { session: CouncilSessionRow; outputs: CouncilOutputRow[]; spent_usd: number; running: boolean }

export interface PersonaOutput {
  abstain: boolean; abstain_reason: string
  observations: { text: string; evidence: string[]; quote?: string; about?: string }[] | null
  hypotheses: { claim: string; evidence: string[]; counter_hypothesis: string; confidence: string; falsified_by: string }[] | null
  open_questions: string[] | null; data_needed: string[] | null
}
export interface ModeratorOutput {
  agreements: { point: string; personas: string[]; evidence: string[] }[] | null
  disagreements: { topic: string; meaning?: string; positions: { persona: string; view: string; evidence: string[] }[] }[] | null
  gaps: string[] | null
}
export interface SynthesisOutput {
  summary: string
  sections?: { title: string; text: string; evidence: string[] | null }[] | null
  evolution?: string
  evolution_evidence?: string[] | null
  convergences: { text: string; evidence: string[] }[] | null
  divergences: { text: string; evidence: string[] }[] | null
  alternative_explanations: string[] | null
  open_questions: string[] | null; data_needed: string[] | null; confidence: string; limits: string
}
export interface VerificationOutput { results: { id: string; support: 'supported' | 'partial' | 'unsupported'; note: string }[] }
export interface DigestOutput { period: string; summary: string; tone: string; evidence: string[] | null }
export interface AnswerOutput { answer: string; evidence: string[] | null; abstain: boolean; abstain_reason: string; limits: string }

export interface TopicRep { id: number; conv_id: number; ts: string; text: string }
export interface Topic { size: number; share: number; rep: TopicRep; keywords: string[] }
export interface Topics {
  coverage: { Embedded?: number; embedded?: number; total?: number; Total?: number }
  overall: Topic[]
  periods: { from: string; to: string; kind: 'surge' | 'lull'; messages: number; topics: Topic[] }[]
}

export interface ChatSession { id: string; title: string; person_id?: number; created_at: string; updated_at: string; spent_usd: number }
export interface ChatCard { type: 'nav' | 'action'; label?: string; to?: string; action_id?: string; summary?: string; danger?: boolean }
export interface ChatMeta { cards?: ChatCard[]; tool?: string; routed?: boolean; safety?: string; budget?: boolean }
export interface ChatMessage { id: number; session_id: string; role: 'user' | 'assistant' | 'tool' | 'notice'; content: string; meta?: ChatMeta; cost_usd: number; created_at: string }
export interface ChatAction { id: string; session_id: string; tool: string; summary: string; danger: boolean; status: 'pending' | 'claimed' | 'done' | 'rejected' | 'failed'; result?: string }
export interface ChatDetail { session: ChatSession; messages: ChatMessage[]; actions: ChatAction[]; cap_usd: number }
export interface PersonNote { id: number; person_id: number; text: string; source: string; created_at: string }

export interface OsintList { id: 'wmn' | 'sherlock' | 'maigret'; name: string; available: boolean; reason?: string; count?: number; skipped?: number; updated_at?: string; source: string; license: string; default_limit: number }
export interface OsintSites { lists: OsintList[] }
export interface OsintRun { id: string; username: string; kind: 'sites' | 'sites:sherlock' | 'sites:maigret' | 'dir_username' | 'dir_full_name'; status: 'running' | 'done' | 'failed' | 'canceled'; total: number; done: number; found: number; failed: number; error?: string; started_at: string; finished_at: string }
export interface OsintFinding { id: number; run_id: string; username?: string; site: string; category: string; url: string; outcome: 'found' | 'failed'; detail?: string; review: 'unreviewed' | 'confirmed' | 'rejected' | 'unsure'; person_id?: number; found_at: string }

export interface EmbedCandidate { name: string; family: string; params?: string; size_mb: number; score: number; reason: string }
export interface EmbedDiscovery { reachable: boolean; base_url: string; openai_url: string; candidates: EmbedCandidate[]; recommended: string; dim?: number; verified: boolean; hint?: string }

export interface PersonOverview {
  id: number; messages: number; first: string; last: string; active_days: number
  platforms: string[]; top_partner?: string; top_share?: number; conversations: number
  night_ratio: number; reply_median_sec: number; peak_hour: number; notes: number
}

export interface OsintLink { engine?: string; label: string; query?: string; url: string }
export interface OsintPlan { query: string; kind: 'username' | 'full_name'; searches: OsintLink[]; direct: OsintLink[]; notice: string }

export interface Pair2 { subject: number; partner: number }
export interface GuideSuggestion { label: string; why: string[]; confidence: 'düşük' | 'orta' }
export interface GuideRow {
  name: string; key: string; conv_id: number; source_id: number; platform: string; total: number
  messages: Pair2; starts: Pair2; sessions: number; first: string; last: string
  latency: { subject: { median: number; n: number }; partner: { median: number; n: number } }
  unsent: Pair2; longest_silence_days: number; night_ratio: number; texts: number
  affection: Pair2; conflict: Pair2; close_friend_hint: boolean; mutual_follow_hint: boolean
  suggestion?: GuideSuggestion; label: string; suggestion_dismissed: boolean
}
export interface Guide { rows: GuideRow[]; label_counts: Record<string, number>; unlabeled: number; suggested: number; labels: string[] }
export interface Highlight { kind: string; title: string; detail?: string; ts: string; message_id: number; weight: number }
export interface MetaTie { handle: string; weight: number; counts: Record<string, number>; first: string; last: string; dm_name?: string; dm_score?: number; dm_total?: number; dm_conv?: number }
export interface MetaView {
  ties: MetaTie[]; hours: number[]; weekday: number[]; events: number; monthly: { month: string; count: number }[]
  follow: { following: number; followers: number; mutual: number; not_following_back: number; fans_not_followed: number }
}
