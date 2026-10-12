import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth/AuthContext'
import { Layout } from './pages/Layout'
import { LoginPage } from './pages/Login'
import { DashboardPage } from './pages/Dashboard'
import { MoviesPage } from './pages/lists/Movies'
import { TvPage } from './pages/lists/Tv'
import { MusicPage } from './pages/lists/Music'
import { BooksPage } from './pages/lists/Books'
import { MovieAddPage } from './pages/add/MovieAdd'
import { TvAddPage } from './pages/add/TvAdd'
import { ArtistAddPage } from './pages/add/ArtistAdd'
import { BookAddPage } from './pages/add/BookAdd'
import { QueuePage } from './pages/queue/Queue'
import { SettingsPage } from './pages/settings/Settings'

// App is the route table. /login is public; everything else requires a valid
// in-memory token (see AuthProvider). The authenticated routes share the Layout
// shell via a nested route element with an <Outlet/>.
function AuthGate() {
  const { me, ready } = useAuth()

  if (!ready) {
    return (
      <div className="login-wrap">
        <p className="dim">Checking session…</p>
      </div>
    )
  }
  if (!me) return <Navigate to="/login" replace />
  return <Layout><Outlet /></Layout>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AuthGate />}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/movies" element={<MoviesPage />} />
        <Route path="/tv" element={<TvPage />} />
        <Route path="/music" element={<MusicPage />} />
        <Route path="/books" element={<BooksPage />} />
        <Route path="/movies/add" element={<MovieAddPage />} />
        <Route path="/tv/add" element={<TvAddPage />} />
        <Route path="/music/add" element={<ArtistAddPage />} />
        <Route path="/books/add" element={<BookAddPage />} />
        <Route path="/queue" element={<QueuePage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
    </Routes>
  )
}
