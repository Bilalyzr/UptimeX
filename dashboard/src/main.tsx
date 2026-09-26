import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { Layout } from './components/Layout';
import { Overview } from './pages/Overview';
import { Endpoints } from './pages/Endpoints';
import { EndpointDetail } from './pages/EndpointDetail';
import { Incidents } from './pages/Incidents';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Overview />} />
          <Route path="endpoints" element={<Endpoints />} />
          <Route path="endpoints/:id" element={<EndpointDetail />} />
          <Route path="incidents" element={<Incidents />} />
          <Route
            path="*"
            element={
              <div className="empty">
                <h2>404</h2>
                <p>Unknown page.</p>
              </div>
            }
          />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
);
