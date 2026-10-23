export const cn = (...v: (string | false | null | undefined)[]) => v.filter(Boolean).join(' ')
