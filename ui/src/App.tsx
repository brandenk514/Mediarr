import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import { PATHS } from './nav'
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
import { ArtistDetailPage } from './pages/music/ArtistDetail'
import { AuthorDetailPage } from './pages/books/AuthorDetail'
import { BookshelvesPage } from './pages/books/Bookshelves'
import { BookSeriesDetailPage, SeriesIndexPage } from './pages/books/Series'
import { UnmappedBooksFilesPage } from './pages/books/UnmappedFiles'
import { UnmappedFilesPage } from './pages/music/UnmappedFiles'
import { LibraryImportPage } from './pages/tv/LibraryImport'
import { CalendarPage } from './pages/Calendar'
import { HistoryPage } from './pages/activity/History'
import { BlocklistPage } from './pages/activity/Blocklist'
import { WantedPage } from './pages/wanted/Wanted'
import { SystemPage } from './pages/system/System'
import { IndexerHubPage } from './pages/indexers/IndexerHub'
import { IndexerStatsPage } from './pages/indexers/IndexerStats'
import { ReleaseSearchPage } from './pages/indexers/ReleaseSearch'
import { SearchHistoryPage } from './pages/indexers/SearchHistory'
import { GlobalSearchPage } from './pages/GlobalSearch'

// App is the route table. /login is public; everything else requires a valid
// in-memory token (see AuthProvider). The authenticated routes share the
// unified Layout shell (top bar + sidebar) via a nested route element with an
// <Outlet/>. Every route path comes from PATHS in nav.ts — the single source
// of truth the sidebar (pages/Layout.tsx) also derives from, so the rail and
// the route tree can't drift (issue #116).
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
        {/* R7 home decision: default landing = the Movies list (the Radarr
            users' home). The cross-kind Dashboard is a Mediarr extra,
            reachable from the sidebar's Dashboard item. */}
        <Route path={PATHS.home} element={<Navigate to={PATHS.movie} replace />} />
        <Route path={PATHS.dashboard} element={<DashboardPage />} />
        {/* Per-kind libraries + Add (four-kind app: TV / Movies / Music / Books).
            The kind is a facet under the Library sidebar section (#116), not a
            top-level switch; every per-kind page stays routed. */}
        <Route path={PATHS.movie} element={<MoviesPage />} />
        <Route path={PATHS.tv} element={<TvPage />} />
        <Route path={PATHS.music} element={<MusicPage />} />
        <Route path={PATHS.book} element={<BooksPage />} />
        <Route path={PATHS.movieAdd} element={<MovieAddPage />} />
        <Route path={PATHS.tvAdd} element={<TvAddPage />} />
        <Route path={PATHS.musicAdd} element={<ArtistAddPage />} />
        {/* Lidarr L5: the Unmapped Files page (static, ranks above
            /music/:id). */}
        <Route path={PATHS.musicUnmapped} element={<UnmappedFilesPage />} />
        {/* Lidarr L1: the artist detail deep route (the static /music/add
            ranks above /music/:id). */}
        <Route path={PATHS.musicDetail} element={<ArtistDetailPage />} />
        <Route path={PATHS.bookAdd} element={<BookAddPage />} />
        {/* Readarr B3: the Unmapped Files page (static, ranks above
            /books/:id). */}
        <Route path={PATHS.bookUnmapped} element={<UnmappedBooksFilesPage />} />
        {/* Readarr B4: the Bookshelves + Series pages (static paths rank
            above the /books/:id deep route, same as /books/add). */}
        <Route path={PATHS.bookBookshelves} element={<BookshelvesPage />} />
        <Route path={PATHS.bookSeries} element={<SeriesIndexPage />} />
        <Route path={PATHS.bookSeriesDetail} element={<BookSeriesDetailPage />} />
        {/* Readarr B1: the author detail deep route (the static /books/add
            ranks above /books/:id). */}
        <Route path={PATHS.bookDetail} element={<AuthorDetailPage />} />
        {/* Radarr R3: static collection/discover routes rank above the
            /movies/:id deep route (same as /movies/add). */}
        <Route path={PATHS.movieCollections} element={<MovieCollectionsPage />} />
        <Route path={PATHS.movieDiscover} element={<MovieDiscoverPage />} />
        {/* Radarr R4: the movie calendar is a first-class Movies sub-page;
            its static path ranks above the /movies/:id deep route. */}
        <Route path={PATHS.movieCalendar} element={<CalendarPage kind="movie" />} />

        {/* Movie deep route (static paths like /movies/add rank above). */}
        <Route path={PATHS.movieDetail} element={<MovieDetailPage />} />
        {/* TV deep routes (static paths rank above /tv/:id). */}
        <Route path={PATHS.tvImport} element={<LibraryImportPage />} />
        <Route path={PATHS.tvDetail} element={<SeriesDetailPage />} />
        {/* Shared: Calendar. */}
        <Route path={PATHS.calendar} element={<CalendarPage />} />
        {/* Shared: Activity. /queue is kept (existing e2e) alongside /activity/queue. */}
        <Route path={PATHS.queue} element={<QueuePage />} />
        <Route path={PATHS.activityQueue} element={<QueuePage />} />
        <Route path={PATHS.activityHistory} element={<HistoryPage />} />
        <Route path={PATHS.activityBlocklist} element={<BlocklistPage />} />
        {/* Shared: Wanted (routed; reached by direct URL — the v0.4.0 sidebar
            (#116) is the fixed eight sections, and Wanted is not one of them). */}
        <Route path={PATHS.wantedMissing} element={<WantedPage tab="missing" />} />
        <Route path={PATHS.wantedCutoff} element={<WantedPage tab="cutoff" />} />
        {/* Prowlarr: manual search + query history (Indexers sub-links in the
            v0.4.0 sidebar; /indexers/stats is static; there is no
            /indexers/:id deep route). */}
        <Route path={PATHS.search} element={<ReleaseSearchPage />} />
        {/* Global cross-kind search (issue #117): the top-bar global search
            lands here with ?q= in the URL. The static /search/all ranks
            above nothing conflicting — PATHS.search (/search) is an exact
            path, and the two are distinct routes. */}
        <Route path={PATHS.globalSearch} element={<GlobalSearchPage />} />
        <Route path={PATHS.history} element={<SearchHistoryPage />} />
        <Route path={PATHS.indexerStats} element={<IndexerStatsPage />} />
        <Route path={PATHS.indexers} element={<IndexerHubPage />} />
        {/* Settings IA (13 sections; default section is media-management). */}
        <Route path={PATHS.settings} element={<Navigate to="/settings/media-management" replace />} />
        <Route path={PATHS.settingsSection} element={<SettingsShell />} />
        {/* System (status / tasks / backup / updates / events / logs). */}
        <Route path={PATHS.system} element={<Navigate to="/system/status" replace />} />
        <Route path={PATHS.systemPage} element={<SystemPage />} />
      </Route>
    </Routes>
  )
}
