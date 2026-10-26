import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth/AuthContext'
import { Layout } from './pages/Layout'
import { LoginPage } from './pages/Login'
import { DashboardPage } from './pages/Dashboard'
import { MoviesPage } from './pages/lists/Movies'
import { TvPage } from './pages/lists/Tv'
import { MusicPage } from './pages/lists/Music'
import { BooksPage } from './pages/lists/Books'
import { MovieAddPage } from './pages/add/MovieAdd'
import { MovieCollectionsPage } from './pages/movies/Collections'
import { MovieDiscoverPage } from './pages/movies/Discover'
import { TvAddPage } from './pages/add/TvAdd'
import { ArtistAddPage } from './pages/add/ArtistAdd'
import { BookAddPage } from './pages/add/BookAdd'
import { QueuePage } from './pages/queue/Queue'
import { SettingsShell } from './pages/settings/SettingsShell'
import { MovieDetailPage } from './pages/movies/MovieDetail'
import { SeriesDetailPage } from './pages/tv/SeriesDetail'
import { LibraryImportPage } from './pages/tv/LibraryImport'
import { CalendarPage } from './pages/Calendar'
import { HistoryPage } from './pages/activity/History'
import { BlocklistPage } from './pages/activity/Blocklist'
import { WantedPage } from './pages/wanted/Wanted'
import { SystemPage } from './pages/system/System'

// App is the route table. /login is public; everything else requires a valid
// in-memory token (see AuthProvider). The authenticated routes share the
// Sonarr-parity Layout shell (top bar + left rail) via a nested route element
// with an <Outlet/>. Route paths follow the Sonarr IA (docs/sonarr-parity-plan.md):
// per-kind library + add + deep routes, then shared Calendar / Activity /
// Wanted / Settings / System areas.
function AuthGate() {
  const { me, ready } = useAuth()
  const location = useLocation()

  if (!ready) {
    return (
      <div className="login-wrap">
        <p className="dim">Checking session…</p>
      </div>
    )
  }
  // Remember where the user was headed so LoginPage can send them back
  // after a successful sign-in (instead of leaving them at /login).
  if (!me) return <Navigate to="/login" replace state={{ from: location }} />
  return <Layout><Outlet /></Layout>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthGate />}>
        {/* P7 home decision: default landing = the TV library (the Sonarr
            users' home). The cross-kind Dashboard is a Mediarr extra,
            reachable from the rail's Dashboard item. */}
        <Route path="/" element={<Navigate to="/tv" replace />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        {/* Per-kind libraries + Add (four-kind app: TV / Movies / Music / Books). */}
        <Route path="/movies" element={<MoviesPage />} />
        <Route path="/tv" element={<TvPage />} />
        <Route path="/music" element={<MusicPage />} />
        <Route path="/books" element={<BooksPage />} />
        <Route path="/movies/add" element={<MovieAddPage />} />
        <Route path="/tv/add" element={<TvAddPage />} />
        <Route path="/music/add" element={<ArtistAddPage />} />
        <Route path="/books/add" element={<BookAddPage />} />
        {/* Radarr R3: static collection/discover routes rank above the
            /movies/:id deep route (same as /movies/add). */}
        <Route path="/movies/collections" element={<MovieCollectionsPage />} />
        <Route path="/movies/discover" element={<MovieDiscoverPage />} />
        {/* Radarr R4: the movie calendar is a first-class Movies sub-page;
            its static path ranks above the /movies/:id deep route. */}
        <Route path="/movies/calendar" element={<CalendarPage kind="movie" />} />

        {/* Movie deep route (static paths like /movies/add rank above). */}
        <Route path="/movies/:id" element={<MovieDetailPage />} />
        {/* TV deep routes (static paths rank above /tv/:id). */}
        <Route path="/tv/import" element={<LibraryImportPage />} />
        <Route path="/tv/:id" element={<SeriesDetailPage />} />
        {/* Shared: Calendar. */}
        <Route path="/calendar" element={<CalendarPage />} />
        {/* Shared: Activity. /queue is kept (existing e2e) alongside /activity/queue. */}
        <Route path="/queue" element={<QueuePage />} />
        <Route path="/activity/queue" element={<QueuePage />} />
        <Route path="/activity/history" element={<HistoryPage />} />
        <Route path="/activity/blocklist" element={<BlocklistPage />} />
        {/* Shared: Wanted. */}
        <Route path="/wanted/missing" element={<WantedPage tab="missing" />} />
        <Route path="/wanted/cutoff" element={<WantedPage tab="cutoff" />} />
        {/* Settings IA (13 sections; default section is media-management). */}
        <Route path="/settings" element={<Navigate to="/settings/media-management" replace />} />
        <Route path="/settings/:section" element={<SettingsShell />} />
        {/* System (status / tasks / backup / updates / events / logs). */}
        <Route path="/system" element={<Navigate to="/system/status" replace />} />
        <Route path="/system/:page" element={<SystemPage />} />
      </Route>
    </Routes>
  )
}
