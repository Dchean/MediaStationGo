import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import type Hls from 'hls.js'
import { ArrowLeft, RefreshCw, Sparkles } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { hlsURL, streamURL } from '../api/client'
import { playbackAPI } from '../api/playback'
import { subtitlesAPI, type SubtitleTrack } from '../api/subtitles'
import type { Media } from '../types'

type Mode = 'direct' | 'hls'

// Fullscreen, dark-themed video page.
//
//   ?mode=hls       force HLS even when direct play would work
//   ?mode=direct    force direct play (default for browser-friendly codecs)
//
// We pick a sensible default based on the source codec: H.264 + AAC in
// MP4 / WebM containers play directly; everything else (HEVC, MKV, AV1,
// AC3 audio, …) gets routed through ffmpeg → HLS.
//
// External subtitles next to the source file are auto-discovered and
// attached as <track> elements.
export function PlayerPage() {
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()

  const ref = useRef<HTMLVideoElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const lastSentRef = useRef(0)

  const [media, setMedia] = useState<Media | null>(null)
  const [mode, setMode] = useState<Mode>('direct')
  const [subs, setSubs] = useState<SubtitleTrack[]>([])

  // Load metadata and pick a default mode.
  useEffect(() => {
    if (!id) return
    mediaAPI.get(id).then((m) => {
      setMedia(m)
      const forced = params.get('mode') as Mode | null
      const auto = pickMode(m)
      setMode(forced ?? auto)
    })
    subtitlesAPI
      .list(id)
      .then(setSubs)
      .catch(() => setSubs([]))
  }, [id, params])

  // Wire up the actual <video> element when we know the mode.
  useEffect(() => {
    if (!media || !ref.current) return
    teardownHls()

    const video = ref.current
    if (mode === 'hls') {
      const url = hlsURL(media.id)
      void import('hls.js').then(({ default: HlsCtor }) => {
        if (HlsCtor.isSupported()) {
          const hls = new HlsCtor({ enableWorker: true, lowLatencyMode: false })
          hls.loadSource(url)
          hls.attachMedia(video)
          hls.on(HlsCtor.Events.ERROR, (_, data) => {
            if (data.fatal) {
              toast.error('HLS 播放失败,尝试切换到直接播放')
              setMode('direct')
              params.set('mode', 'direct')
              setParams(params, { replace: true })
            }
          })
          hlsRef.current = hls
        } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
          video.src = url
        } else {
          toast.error('当前浏览器不支持 HLS,降级到直接播放')
          setMode('direct')
        }
        void video.play().catch(() => undefined)
      })
    } else {
      video.src = streamURL(media.id)
      void video.play().catch(() => undefined)
    }
    return teardownHls
  }, [media, mode, params, setParams])

  // Persist resume position every 10 seconds while playing.
  useEffect(() => {
    if (!media || !ref.current) return
    const video = ref.current
    const handler = () => {
      const now = Date.now()
      if (now - lastSentRef.current < 10_000) return
      lastSentRef.current = now
      const positionMs = Math.floor(video.currentTime * 1000)
      const durationMs = Math.floor((video.duration || 0) * 1000)
      if (positionMs > 0) {
        playbackAPI.recordProgress(media.id, positionMs, durationMs).catch(() => undefined)
      }
    }
    video.addEventListener('timeupdate', handler)
    video.addEventListener('pause', handler)
    return () => {
      video.removeEventListener('timeupdate', handler)
      video.removeEventListener('pause', handler)
    }
  }, [media])

  // ESC = back.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') navigate(-1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])

  const teardownHls = () => {
    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }
  }

  return (
    <div className="-m-6 flex min-h-screen flex-col bg-black md:-m-8">
      <button
        onClick={() => navigate(-1)}
        className="absolute left-4 top-4 z-10 flex items-center gap-2 rounded-full bg-black/60 px-3 py-1.5 text-sm text-ink-600 backdrop-blur transition hover:bg-black/80"
      >
        <ArrowLeft size={16} /> 返回
      </button>

      <button
        onClick={() => {
          const next = mode === 'hls' ? 'direct' : 'hls'
          setMode(next)
          params.set('mode', next)
          setParams(params, { replace: true })
        }}
        className="absolute right-4 top-4 z-10 flex items-center gap-2 rounded-full bg-black/60 px-3 py-1.5 text-sm text-ink-600 backdrop-blur transition hover:bg-black/80"
        title="切换播放模式"
      >
        {mode === 'hls' ? (
          <>
            <RefreshCw size={14} /> HLS 转码中
          </>
        ) : (
          <>
            <Sparkles size={14} /> 直接播放
          </>
        )}
      </button>

      <div className="flex flex-1 items-center justify-center">
        {media ? (
          <video
            ref={ref}
            controls
            autoPlay
            playsInline
            className="max-h-screen w-full max-w-[1600px] bg-black"
            onError={() => {
              // 浏览器对 <video src> 的错误描述非常有限，把详细原因
              // 转给开发者控制台 + 一条 toast；常见原因是 codec 不支持。
              if (mode === 'direct') {
                toast.error('直接播放失败，切换到 HLS 转码')
                setMode('hls')
                params.set('mode', 'hls')
                setParams(params, { replace: true })
              } else {
                toast.error('视频播放失败，请检查文件是否存在')
              }
            }}
          >
            {subs.map((t, i) => (
              <track
                key={t.path}
                kind="subtitles"
                src={subtitlesAPI.url(media.id, t.path)}
                srcLang={t.lang}
                label={t.label || t.lang}
                default={i === 0}
              />
            ))}
          </video>
        ) : (
          <p className="text-sand-500">加载中…</p>
        )}
      </div>
    </div>
  )
}

const directContainers = ['mp4', 'webm', 'm4v']
const directVideoCodecs = ['h264', 'avc', 'avc1']
const directAudioCodecs = ['aac', 'mp3', 'opus']

function pickMode(m: Media): Mode {
  const c = (m.container ?? '').toLowerCase()
  const v = (m.video_codec ?? '').toLowerCase()
  const a = (m.audio_codec ?? '').toLowerCase()
  const containerOK = directContainers.some((x) => c.includes(x))
  const videoOK = !v || directVideoCodecs.some((x) => v.includes(x))
  const audioOK = !a || directAudioCodecs.some((x) => a.includes(x))
  return containerOK && videoOK && audioOK ? 'direct' : 'hls'
}
