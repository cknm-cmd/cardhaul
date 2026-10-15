export function bytes(n: number): string {
  if (!n || n < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1000 && i < units.length - 1) { n /= 1000; i++ }
  return `${n.toFixed(i < 2 ? 0 : 1)} ${units[i]}`
}

export function duration(seconds: number): string {
  if (seconds == null || seconds < 0) return 'calculating…'
  const s = Math.round(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h) return `${h}h ${String(m).padStart(2, '0')}m`
  if (m) return `${m}m`
  return 'under a minute'
}

export function when(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return ''
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function shortName(path: string): string {
  const base = path.split('/').pop() ?? path
  return base.length > 32 ? base.slice(0, 14) + '…' + base.slice(-14) : base
}
