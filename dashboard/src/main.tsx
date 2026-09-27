import React, { lazy, Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { AuthProvider } from './auth';
import { Layout } from './components/Layout';
import { RequireAuth } from './components/RequireAuth';

// Each page is its own lazily-loaded chunk: routes load independently,
// direct URLs work, and the bundle stays off the critical path.
const Landing = lazy(() => import('./pages/Landing').then((m) => ({ default: m.Landing })));
const Login = lazy(() => import('./pages/Auth').then((m) => ({ default: m.Login })));
const Signup = lazy(() => import('./pages/Auth').then((m) => ({ default: m.Signup })));
const Overview = lazy(() => import('./pages/Overview').then((m) => ({ default: m.Overview })));
const Endpoints = lazy(() => import('./pages/Endpoints').then((m) => ({ default: m.Endpoints })));
const EndpointDetail = lazy(() =>
  import('./pages/EndpointDetail').then((m) => ({ default: m.EndpointDetail })),
);
const Incidents = lazy(() => import('./pages/Incidents').then((m) => ({ default: m.Incidents })));
const Billing = lazy(() => import('./pages/Billing').then((m) => ({ default: m.Billing })));
const Profile = lazy(() => import('./pages/Profile').then((m) => ({ default: m.Profile })));
const PublicStatus = lazy(() =>
  import('./pages/PublicStatus').then((m) => ({ default: m.PublicStatus })),
);

const PageFallback = () => <div className="loading skeleton" role="status" aria-label="Loading page" />;

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AuthProvider>
      {/* v7 future flags: opt in early to the upcoming React Router v7
          behaviors (startTransition wrapping + splat-relative resolution)
          and silence the deprecation warnings. */}
      <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <Suspense fallback={<PageFallback />}>
          <Routes>
            <Route path="/" element={<Landing />} />
            <Route path="/login" element={<Login />} />
            <Route path="/signup" element={<Signup />} />
            <Route path="/status/:slug" element={<PublicStatus />} />
            <Route
              path="/app"
              element={
                <RequireAuth>
                  <Layout />
                </RequireAuth>
              }
            >
              <Route index element={<Overview />} />
              <Route path="endpoints" element={<Endpoints />} />
              <Route path="endpoints/:id" element={<EndpointDetail />} />
            <Route path="incidents" element={<Incidents />} />
            <Route path="billing" element={<Billing />} />
            <Route path="profile" element={<Profile />} />
          </Route>
            <Route path="/app/*" element={<Navigate to="/app" replace />} />
            <Route
              path="*"
              element={
                <div className="empty">
                  <h2>404</h2>
                  <p>Unknown page.</p>
                </div>
              }
            />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </AuthProvider>
  </React.StrictMode>,
);

// Dismiss the ChainGPT-style boot curtain once React has mounted: wipe the
// dark loader away to reveal the app beneath.
const boot = document.getElementById('boot-loader');
if (boot) {
  window.setTimeout(() => {
    boot.classList.add('bl-hide');
    window.setTimeout(() => boot.remove(), 700);
  }, 1100);
}
