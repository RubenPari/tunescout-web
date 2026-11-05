import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In sviluppo le chiamate /api vanno al server Go (porta 8080).
// Avvia il server con BASE_URL=http://127.0.0.1:5173 così il login Spotify
// torna sul dev server.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: { '/api': { target: 'http://127.0.0.1:8080' } },
  },
})
