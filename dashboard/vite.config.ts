import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev server proxies API calls to the Go monitor, so the dashboard uses
// relative URLs everywhere and works unchanged behind nginx in production.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: process.env.VITE_MONITOR_URL || 'http://localhost:8080', changeOrigin: true },
      '/health': { target: process.env.VITE_MONITOR_URL || 'http://localhost:8080', changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
  },
} as never);
