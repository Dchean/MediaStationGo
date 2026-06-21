const ARTWORK_CACHE = 'mediastationgo-artwork-v1'
const STRIP_QUERY_KEYS = ['token', 'profile_id', 'profile_pin_token']

function isArtworkRequest(url) {
  if (url.origin !== self.location.origin) return false
  if (url.pathname === '/api/img') return true
  return url.pathname.startsWith('/api/cloud/play/')
}

function normalizedArtworkRequest(request) {
  const url = new URL(request.url)
  for (const key of STRIP_QUERY_KEYS) {
    url.searchParams.delete(key)
  }
  return new Request(url.toString(), {
    method: 'GET',
    headers: {
      Accept: request.headers.get('Accept') || 'image/avif,image/webp,image/apng,image/*,*/*;q=0.8',
    },
    credentials: 'same-origin',
    mode: 'same-origin',
    redirect: 'follow',
  })
}

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (!isArtworkRequest(url)) return

  event.respondWith(cacheArtwork(request))
})

async function cacheArtwork(request) {
  const cache = await caches.open(ARTWORK_CACHE)
  const cacheKey = normalizedArtworkRequest(request)
  const cached = await cache.match(cacheKey)
  if (cached) return cached

  const response = await fetch(request)
  const contentType = response.headers.get('Content-Type') || ''
  if (response.ok && contentType.toLowerCase().startsWith('image/')) {
    await cache.put(cacheKey, response.clone())
  }
  return response
}
