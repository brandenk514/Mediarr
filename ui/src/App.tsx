import { Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth/AuthContext'
import { Layout } from './pages/Layout'
import { LoginPage } from './pages/Login'

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
        <Route path="/" element={<PageStub kind="Dashboard" />} />
        <Route path="/movies" element={<PageStub kind="Movies" />} />
        <Route path="/tv" element={<PageStub kind="TV" />} />
        <Route path="/music" element={<PageStub kind="Music" />} />
        <Route path="/books" element={<PageStub kind="Books" />} />
        <Route path="/queue" element={<PageStub kind="Queue" />} />
        <Route path="/settings" element={<PageStub kind="Settings" />} />
      </Route>
    </Routes>
  )
}

// PageStub is the placeholder for a route whose feature lands in a later M6
// issue (#40–#44). Each one is named after its board issue.
function PageStub({ kind }: { kind: string }) {
  return (
    <div className="card">
      <h2>{kind}</h2>
      <p className="dim">
        This view lands in a later M6 issue. The REST API it drives already exists —
        the typed client in <code>ui/src/api/client.ts</code> is the reference.
      </p>
    </div>
  )
}
