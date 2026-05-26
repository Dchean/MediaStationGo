import { Github, Globe, Send } from 'lucide-react'

const LINKS = [
  { href: 'https://github.com/ShukeBta/MediaStationGo', icon: Github, label: '开源仓库' },
  { href: 'https://github.com/ShukeBta', icon: Globe, label: '作者主页' },
  { href: 'https://t.me/MediaStationGo', icon: Send, label: 'TG 群组' },
]

export function AppFooter({ className = '' }: { className?: string }) {
  return (
    <footer className={`flex items-center justify-center gap-1 ${className}`}>
      {LINKS.map((link, i) => (
        <span key={link.href} className="flex items-center">
          {i > 0 && <span className="mx-2 h-3 w-px bg-sand-300" />}
          <a
            href={link.href}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs text-sand-500 transition-colors hover:bg-sand-200 hover:text-ink-50"
            title={link.label}
          >
            <link.icon size={14} />
            <span>{link.label}</span>
          </a>
        </span>
      ))}
    </footer>
  )
}
