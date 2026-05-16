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
const DownloadsPage = lazy(() =>
  import('./pages/DownloadsPage').then((m) => ({ default: m.DownloadsPage })),
)
const SubscriptionsPage = lazy(() =>
  import('./pages/SubscriptionsPage').then((m) => ({ default: m.SubscriptionsPage })),
)
const ProfilePage = lazy(() =>
  import('./pages/ProfilePage').then((m) => ({ default: m.ProfilePage })),
)
const StatsPage = lazy(() => import('./pages/StatsPage').then((m) => ({ default: m.StatsPage })))
const DiscoverPage = lazy(() =>
  import('./pages/DiscoverPage').then((m) => ({ default: m.DiscoverPage })),
)
const TasksPage = lazy(() => import('./pages/TasksPage').then((m) => ({ default: m.TasksPage })))
const RecycleBinPage = lazy(() =>
  import('./pages/RecycleBinPage').then((m) => ({ default: m.RecycleBinPage })),
)
const DlnaPage = lazy(() => import('./pages/DlnaPage').then((m) => ({ default: m.DlnaPage })))
const FileManagerPage = lazy(() =>
  import('./pages/FileManagerPage').then((m) => ({ default: m.FileManagerPage })),
)
const APIConfigsPage = lazy(() =>
  import('./pages/APIConfigsPage').then((m) => ({ default: m.APIConfigsPage })),
)
const StoragePage = lazy(() =>
  import('./pages/StoragePage').then((m) => ({ default: m.StoragePage })),
)
const DuplicatesPage = lazy(() =>
  import('./pages/DuplicatesPage').then((m) => ({ default: m.DuplicatesPage })),
)
const SchedulerPage = lazy(() =>
  import('./pages/SchedulerPage').then((m) => ({ default: m.SchedulerPage })),
)
const WatchHistoryPage = lazy(() =>
  import('./pages/WatchHistoryPage').then((m) => ({ default: m.WatchHistoryPage })),
)
const PosterWallPage = lazy(() =>
  import('./pages/PosterWallPage').then((m) => ({ default: m.PosterWallPage })),
)
const SitesPage = lazy(() =>
  import('./pages/SitesPage').then((m) => ({ default: m.SitesPage })),
)
const SiteSearchPage = lazy(() =>
  import('./pages/SiteSearchPage').then((m) => ({ default: m.SiteSearchPage })),
)
const AIAssistantPage = lazy(() =>
  import('./pages/AIAssistantPage').then((m) => ({ default: m.AIAssistantPage })),
)
const StrmPage = lazy(() =>
  import('./pages/StrmPage').then((m) => ({ default: m.StrmPage })),
)
const ToolsPage = lazy(() =>
  import('./pages/ToolsPage').then((m) => ({ default: m.ToolsPage })),
)
const ProfileManagementPage = lazy(() =>
  import('./pages/ProfileManagementPage').then((m) => ({ default: m.ProfileManagementPage })),
)
const NotifyChannelsPage = lazy(() =>
  import('./pages/NotifyChannelsPage').then((m) => ({ default: m.NotifyChannelsPage })),
)
const SettingsPage = lazy(() =>
  import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })),
)
const AssistantChatPage = lazy(() =>
  import('./pages/AssistantChatPage').then((m) => ({ default: m.AssistantChatPage })),
)
const DownloadClientsPage = lazy(() =>
  import('./pages/DownloadClientsPage').then((m) => ({ default: m.DownloadClientsPage })),
)
const LicensePage = lazy(() =>
  import('./pages/LicensePage').then((m) => ({ default: m.LicensePage })),
)
const StorageConfigPage = lazy(() =>
  import('./pages/StorageConfigPage').then((m) => ({ default: m.StorageConfigPage })),
)

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
          <Route path="discover" element={<DiscoverPage />} />
          <Route path="search" element={<SearchPage />} />
          <Route path="favourites" element={<FavouritesPage />} />
          <Route path="playlists" element={<PlaylistsPage />} />
          <Route path="playlist/:id" element={<PlaylistDetailPage />} />
          <Route path="media/:id" element={<MediaDetailPage />} />
          <Route path="play/:id" element={<PlayerPage />} />
          <Route path="downloads" element={<DownloadsPage />} />
          <Route path="subscriptions" element={<SubscriptionsPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="dlna" element={<DlnaPage />} />
          <Route path="history" element={<WatchHistoryPage />} />
          <Route path="poster-wall" element={<PosterWallPage />} />
          <Route path="sites" element={<SitesPage />} />
          <Route path="site-search" element={<SiteSearchPage />} />
          <Route path="ai" element={<AIAssistantPage />} />
          <Route path="play-profiles" element={<ProfileManagementPage />} />
          <Route
            path="files"
            element={
              <RequireAdmin>
                <FileManagerPage />
              </RequireAdmin>
            }
          />
          <Route
            path="storage"
            element={
              <RequireAdmin>
                <StoragePage />
              </RequireAdmin>
            }
          />
          <Route
            path="duplicates"
            element={
              <RequireAdmin>
                <DuplicatesPage />
              </RequireAdmin>
            }
          />
          <Route
            path="scheduler"
            element={
              <RequireAdmin>
                <SchedulerPage />
              </RequireAdmin>
            }
          />
          <Route
            path="api-configs"
            element={
              <RequireAdmin>
                <APIConfigsPage />
              </RequireAdmin>
            }
          />
          <Route
            path="tasks"
            element={
              <RequireAdmin>
                <TasksPage />
              </RequireAdmin>
            }
          />
          <Route
            path="recycle"
            element={
              <RequireAdmin>
                <RecycleBinPage />
              </RequireAdmin>
            }
          />
          <Route
            path="strm"
            element={
              <RequireAdmin>
                <StrmPage />
              </RequireAdmin>
            }
          />
          <Route
            path="tools"
            element={
              <RequireAdmin>
                <ToolsPage />
              </RequireAdmin>
            }
          />
          <Route
            path="notify-channels"
            element={
              <RequireAdmin>
                <NotifyChannelsPage />
              </RequireAdmin>
            }
          />
          <Route
            path="settings"
            element={
              <RequireAdmin>
                <SettingsPage />
              </RequireAdmin>
            }
          />
          <Route
            path="assistant"
            element={
              <RequireAdmin>
                <AssistantChatPage />
              </RequireAdmin>
            }
          />
          <Route
            path="download-clients"
            element={
              <RequireAdmin>
                <DownloadClientsPage />
              </RequireAdmin>
            }
          />
          <Route
            path="license"
            element={
              <RequireAdmin>
                <LicensePage />
              </RequireAdmin>
            }
          />
          <Route
            path="storage-config"
            element={
              <RequireAdmin>
                <StorageConfigPage />
              </RequireAdmin>
            }
          />
          <Route
            path="stats"
            element={
              <RequireAdmin>
                <StatsPage />
              </RequireAdmin>
            }
          />
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
