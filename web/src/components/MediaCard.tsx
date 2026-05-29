import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Film, Play, Layers, Star } from 'lucide-react'
import { imageURL } from '../api/client'
import type { Media } from '../types'

export const MediaCard = ({
  media, progress, count, rating, linkTo, onClick,
}: {
  media: Media
  progress?: number
  count?: number
  rating?: number
  linkTo?: string
  onClick?: () => void
}) => {
  const ref = useRef<HTMLDivElement>(null)
  const href = linkTo ?? `/media/${media.id}`
  const [posterFit, setPosterFit] = useState<'cover' | 'contain'>('cover')

  useEffect(() => {
    setPosterFit('cover')
  }, [media.poster_url])

  const card = (
      <motion.div
        ref={ref}
        whileHover={{ scale: 1.04, y: -6 }}
        transition={{ type: 'spring', stiffness: 280, damping: 22 }}
        className="relative overflow-hidden rounded-2xl bg-white border border-gray-200/90 shadow-[0_1px_3px_rgba(0,0,0,0.01),0_1px_2px_rgba(0,0,0,0.015)] transition-all duration-300 hover:border-brand-500/40 hover:shadow-[0_12px_32px_rgba(17,24,39,0.04),0_4px_12px_rgba(17,24,39,0.01)]"
      >
        {/* Poster Wrapper */}
        <div className="relative aspect-[2/3] w-full overflow-hidden bg-gray-50">
          {media.poster_url ? (
            <>
              {posterFit === 'contain' && (
                <img
                  src={imageURL(media.poster_url)}
                  alt=""
                  aria-hidden="true"
                  loading="lazy"
                  className="absolute inset-0 h-full w-full scale-110 object-cover object-center opacity-25 blur-xl"
                  referrerPolicy="no-referrer"
                />
              )}
              <img
                src={imageURL(media.poster_url)}
                alt={media.title}
                loading="lazy"
                decoding="async"
                onLoad={(event) => {
                  const img = event.currentTarget
                  setPosterFit(img.naturalWidth > img.naturalHeight ? 'contain' : 'cover')
                }}
                className={
                  'relative block h-full w-full object-center transition-transform duration-700 ease-out group-hover:scale-105 ' +
                  (posterFit === 'contain' ? 'object-contain p-1.5' : 'object-cover')
                }
                referrerPolicy="no-referrer"
              />
            </>
          ) : (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-gray-500 bg-gray-50">
              <Film size={28} className="stroke-[1.5]" />
              <span className="text-[10px] uppercase tracking-wider font-bold">No Poster</span>
            </div>
          )}

          {/* Episode count badge */}
          {count !== undefined && count > 1 && (
            <span className="absolute right-3 top-3 inline-flex items-center gap-1 rounded-xl bg-[#111827]/90 px-2 py-1 text-[10px] font-bold text-white shadow-sm border border-gray-200">
              <Layers size={10} className="text-[#c9954a]" />
              <span>{count} 集</span>
            </span>
          )}

          {/* Rating Badge */}
          {(rating || (media as any).rating) && (
            <span className="absolute left-3 top-3 inline-flex items-center gap-0.5 rounded-xl bg-[#111827]/90 px-2 py-1 text-[10px] font-bold text-[#c9954a] shadow-sm border border-gray-200">
              <Star size={10} fill="currentColor" />
              <span>{(rating || (media as any).rating).toFixed(1)}</span>
            </span>
          )}

          {/* Premium Hover Overlay */}
          <div className="absolute inset-0 bg-gradient-to-t from-[#111827]/90 via-[#111827]/30 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300 flex flex-col justify-end p-4">
            <motion.div
              initial={{ y: 15, opacity: 0 }}
              whileHover={{ y: 0, opacity: 1 }}
              transition={{ duration: 0.2 }}
              className="space-y-2"
            >
              <span className="inline-flex items-center gap-1.5 rounded-xl bg-brand-500 px-4 py-2 text-xs font-bold text-white shadow-md shadow-brand-500/20">
                <Play size={10} fill="currentColor" className="text-white" />
                <span>立即观影</span>
              </span>
              <p className="text-[10px] text-gray-500 font-semibold line-clamp-2 leading-relaxed">
                {media.overview || "暂无简介内容"}
              </p>
            </motion.div>
          </div>

          {/* Progress Bar overlay */}
          {progress !== undefined && progress > 0 && progress < 1 && (
            <div className="absolute inset-x-0 bottom-0 h-1.5 bg-gray-200">
              <div 
                className="h-full bg-gradient-to-r from-brand-400 to-brand-500 rounded-r-full transition-all duration-300" 
                style={{ width: `${Math.round(progress * 100)}%` }} 
              />
            </div>
          )}
        </div>

        {/* Media Metadata Info */}
        <div className="p-4 space-y-1 bg-white border-t border-gray-50">
          <p className="truncate text-sm font-bold text-gray-900 group-hover:text-brand-500 transition-colors duration-200">
            {media.title}
          </p>
          <div className="flex items-center justify-between text-[11px] text-gray-500 font-bold uppercase tracking-wider">
            <span>{media.year > 0 ? media.year : "未知年份"}</span>
            {media.video_codec && (
              <span className="px-1.5 py-0.5 rounded-xl bg-gray-50 text-gray-600 border border-gray-100">
                {media.video_codec}
              </span>
            )}
          </div>
        </div>
      </motion.div>
  )

  if (onClick) {
    return (
      <button type="button" onClick={onClick} className="group block w-full text-left">
        {card}
      </button>
    )
  }

  return (
    <Link to={href} className="group block">
      {card}
    </Link>
  )
}
