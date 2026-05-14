import { Suspense, lazy } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'

import { Layout } from './components/Layout'
import { RequireAdmin, RequireAuth } from './components/RequireAuth'
import { LoginPage } from './pages/LoginPage'

// Lazy-loaded routes — the login screen and the layout shell ship in the
// initial bundle; everything else is fetched on first navigation.
const HomePage = lazy(() => import('./pages/HomePage').then((m) => ({ default: m.HomePage })))
const LibraryPage = lazy(() =>
  import('./pages/LibraryPage').then((m) => ({ default: m.LibraryPage })),
)
const SearchPage = lazy(() =>
  import('./pages/SearchPage').then((m) => ({ default: m.SearchPage })),
)
const FavouritesPage = lazy(() =>
  import('./pages/FavouritesPage').then((m) => ({ default: m.FavouritesPage })),
)
const PlaylistsPage = lazy(() =>
  import('./pages/PlaylistsPage').then((m) => ({ default: m.PlaylistsPage })),
)
const PlaylistDetailPage = lazy(() =>
  import('./pages/PlaylistDetailPage').then((m) => ({ default: m.PlaylistDetailPage })),
)
const MediaDetailPage = lazy(() =>
  import('./pages/MediaDetailPage').then((m) => ({ default: m.MediaDetailPage })),
)
const PlayerPage = lazy(() =>
  import('./pages/PlayerPage').then((m) => ({ default: m.PlayerPage })),
)
const AdminPage = lazy(() => import('./pages/AdminPage').then((m) => ({ default: m.AdminPage })))

// Fallback shown while a chunk is loading.
const Loading = () => <p className="px-6 py-8 text-slate-500">加载中…</p>

export default function App() {
  return (
    <Suspense fallback={<Loading />}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <Layout />
            </RequireAuth>
          }
        >
          <Route index element={<HomePage />} />
          <Route path="library/:id" element={<LibraryPage />} />
          <Route path="search" element={<SearchPage />} />
          <Route path="favourites" element={<FavouritesPage />} />
          <Route path="playlists" element={<PlaylistsPage />} />
          <Route path="playlist/:id" element={<PlaylistDetailPage />} />
          <Route path="media/:id" element={<MediaDetailPage />} />
          <Route path="play/:id" element={<PlayerPage />} />
          <Route
            path="admin"
            element={
              <RequireAdmin>
                <AdminPage />
              </RequireAdmin>
            }
          />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  )
}
