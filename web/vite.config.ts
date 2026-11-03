import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({plugins: [react()], server: {proxy: {'/v1': 'http://localhost:8080', '/web': 'http://localhost:8080'}}, build: {rollupOptions: {input: {app:'index.html', offline:'src/offline/worker.ts'}, output: {entryFileNames: chunk => chunk.name === 'offline' ? 'offline-worker.js' : 'assets/[name]-[hash].js'}}}})
