import type {
  AppState, Ego, Guide, Highlight, MetaView, OsintPlan, PersonOverview, EmbedDiscovery, OsintFinding, OsintRun, OsintSites, ChatDetail, ChatMessage, ChatSession, PersonNote, CouncilDetail, EmbedStatus, CouncilPlan, CouncilSessionRow, PersonaInfo, Comparison, Contact, Graph, ImportResult, LinkSuggestion, Person, Profile, SearchHit,
  Risk, Settings, Topics, Source, Spend, TimelineItem,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch('/api' + path, init)
  } catch {
    throw new ApiError('Joe sunucusuna ulaşılamadı. Uygulama çalışıyor mu?', 0)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    throw new ApiError(text.slice(0, 200) || `HTTP ${res.status}`, res.status)
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | undefined)?.error ?? `HTTP ${res.status}`
    throw new ApiError(msg, res.status)
  }
  return data as T
}

export const api = {
  state: () => call<AppState>('GET', '/state'),
  settings: () => call<Settings>('GET', '/settings'),
  saveSettings: (u: Record<string, unknown>) => call<Settings>('PUT', '/settings', u),
  testProvider: () =>
    call<{ ok: boolean; error?: string; reply?: string; ms?: number; tokens?: number; cost_usd?: number }>('POST', '/settings/test'),

  ledger: () => call<Spend>('GET', '/ledger'),
  personas: () => call<{ prompt_version: string; moderator_question: string; personas: PersonaInfo[] }>('GET', '/personas'),
  councilPlan: (body: Record<string, unknown>) => call<CouncilPlan>('POST', '/council/plan', body),
  councilStart: (body: Record<string, unknown>) => call<{ id: string }>('POST', '/council', body),
  councils: () => call<CouncilSessionRow[]>('GET', '/council'),
  council: (id: string) => call<CouncilDetail>('GET', `/council/${id}`),
  councilCancel: (id: string) => call<void>('POST', `/council/${id}/cancel`),
  councilResume: (id: string) => call<void>('POST', `/council/${id}/resume`),
  councilDelete: (id: string) => call<void>('DELETE', `/council/${id}`),
  sources: () => call<Source[]>('GET', '/sources'),
  importPath: (paths: string[]) => call<ImportResult>('POST', '/import', { paths }),
  importFiles: (files: File[]) => {
    const fd = new FormData()
    for (const f of files) fd.append('file', f)
    return call<ImportResult>('POST', '/import', fd)
  },
  deleteSource: (id: number) => call<void>('DELETE', `/sources/${id}`),
  sourcePartners: (id: number) => call<{ created: Person[] }>('POST', `/sources/${id}/partners`),
  setOwner: (id: number, name: string, handle: string) =>
    call<ImportResult>('POST', `/sources/${id}/owner`, { name, handle }),
  contacts: (id: number) => call<Contact[]>('GET', `/sources/${id}/contacts`),

  people: () => call<Person[]>('GET', '/people'),
  peopleOverview: () => call<PersonOverview[]>('GET', '/people/overview'),
  person: (id: number) => call<Person>('GET', `/people/${id}`),
  createPerson: (name: string, identities: string[] = [], note = '') =>
    call<Person>('POST', '/people', { name, identities, note }),
  patchPerson: (id: number, p: Partial<{ name: string; note: string; is_self: boolean }>) =>
    call<Person>('PATCH', `/people/${id}`, p),
  deletePerson: (id: number) => call<void>('DELETE', `/people/${id}`),
  addIdentity: (id: number, value: string) => call<Person>('POST', `/people/${id}/identities`, { value }),
  removeIdentity: (id: number, iid: number) => call<Person>('DELETE', `/people/${id}/identities/${iid}`),
  profile: (id: number) => call<Profile>('GET', `/people/${id}/profile`),
  chats: () => call<ChatSession[]>('GET', '/chats'),
  createChat: (title = '', personId = 0) => call<ChatSession>('POST', '/chats', { title, person_id: personId }),
  chat: (id: string) => call<ChatDetail>('GET', `/chats/${id}`),
  renameChat: (id: string, title: string) => call<ChatSession>('PATCH', `/chats/${id}`, { title }),
  deleteChat: (id: string) => call<void>('DELETE', `/chats/${id}`),
  sendChat: (id: string, text: string) =>
    call<{ messages: ChatMessage[]; actions: ChatDetail['actions']; session: ChatSession; error?: string }>('POST', `/chats/${id}/messages`, { text }),
  confirmAction: (id: string, aid: string) => call<ChatDetail>('POST', `/chats/${id}/actions/${aid}/confirm`),
  rejectAction: (id: string, aid: string) => call<ChatDetail>('POST', `/chats/${id}/actions/${aid}/reject`),
  notes: (personId: number) => call<PersonNote[]>('GET', `/people/${personId}/notes`),
  addNote: (personId: number, text: string) => call<PersonNote>('POST', `/people/${personId}/notes`, { text }),
  deleteNote: (id: number) => call<void>('DELETE', `/notes/${id}`),
  osintSites: () => call<OsintSites>('GET', '/osint/sites'),
  osintUpdateSites: (list: string) => call<{ id: string; count: number; skipped: number }>('POST', '/osint/sites/update', { list }),
  osintRuns: () => call<OsintRun[]>('GET', '/osint/runs'),
  osintStart: (username: string, list = 'wmn', limit?: number) => call<{ id: string }>('POST', '/osint/runs', { username, acknowledge: true, list, ...(limit === undefined ? {} : { limit }) }),
  osintDirectories: (query: string, kind: 'username' | 'full_name') => call<{ id: string }>('POST', '/osint/directories', { query, kind, acknowledge: true }),
  osintPlan: (q: string, kind: 'username' | 'full_name') => call<OsintPlan>('GET', `/osint/plan?q=${encodeURIComponent(q)}&kind=${kind}`),
  osintRun: (id: string) => call<{ run: OsintRun; findings: OsintFinding[] }>('GET', `/osint/runs/${id}`),
  osintCancel: (id: string) => call<void>('POST', `/osint/runs/${id}/cancel`),
  osintDelete: (id: string) => call<void>('DELETE', `/osint/runs/${id}`),
  osintReview: (id: number, review: string) => call<void>('PATCH', `/osint/findings/${id}`, { review }),
  osintAttach: (id: number, personId: number) => call<void>('POST', `/osint/findings/${id}/attach`, { person_id: personId }),
  personOsint: (personId: number) => call<OsintFinding[]>('GET', `/people/${personId}/osint`),
  topics: (id: number) => call<Topics>('GET', `/people/${id}/topics`),
  risk: (id: number) => call<Risk>('GET', `/people/${id}/risk`),
  timeline: (id: number) => call<TimelineItem[]>('GET', `/people/${id}/timeline`),

  graph: (max = 60) => call<Graph>('GET', `/graph?max=${max}`),
  guide: (personId: number) => call<Guide>('GET', `/people/${personId}/guide`),
  setContactLabel: (key: string, label: string, dismiss = false) => call<void>('PUT', '/contact-labels', { key, label, dismiss }),
  highlights: (convId: number, personId: number) => call<{ partner: string; highlights: Highlight[] }>('GET', `/conversations/${convId}/highlights?person=${personId}`),
  meta: (personId: number, max = 40) => call<MetaView>('GET', `/people/${personId}/meta?max=${max}`),
  graphHide: (id: string, label: string) => call<void>('POST', '/graph/hide', { id, label }),
  graphUnhide: (id: string) => call<void>('POST', '/graph/unhide', { id }),
  network: (personId: number, max = 40) => call<Ego>('GET', `/people/${personId}/network?max=${max}`),
  suggestions: () => call<LinkSuggestion[]>('GET', '/suggestions'),
  compare: (a: number, b: number) => call<Comparison>('GET', `/compare?a=${a}&b=${b}`),
  search: (q: string, mode: 'text' | 'semantic' | 'hybrid' = 'text') =>
    call<SearchHit[]>('GET', `/search?q=${encodeURIComponent(q)}${mode === 'text' ? '' : `&mode=${mode}`}`),
  embedStatus: () => call<EmbedStatus>('GET', '/embed/status'),
  embedRun: () => call<void>('POST', '/embed/run'),
  embedDiscover: () => call<EmbedDiscovery>('GET', '/embed/discover'),
  embedAuto: () => call<{ model: string; url: string; dim: number; indexing_started: boolean }>('POST', '/embed/auto'),
  embedCancel: () => call<void>('POST', '/embed/cancel'),
}
