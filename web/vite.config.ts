import {readdir, readFile, writeFile} from 'node:fs/promises'
import {join} from 'node:path'
import {brotliCompressSync, constants, gzipSync} from 'node:zlib'
import {defineConfig, type Plugin} from 'vite'
import react from '@vitejs/plugin-react'

// The Go server serves these copies of fingerprinted assets when the browser
// accepts them, so slow links download a fraction of the bundle.
function precompress(): Plugin {
  let outDir = 'dist'
  return {
    name: 'earnest-precompress',
    apply: 'build',
    configResolved(config) { outDir = config.build.outDir },
    async writeBundle() {
      const assets = join(outDir, 'assets')
      for (const name of await readdir(assets)) {
        if (!/\.(js|css)$/.test(name)) continue
        const file = join(assets, name)
        const data = await readFile(file)
        await writeFile(`${file}.br`, brotliCompressSync(data, {params: {[constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: data.length}}))
        await writeFile(`${file}.gz`, gzipSync(data, {level: 9}))
      }
    },
  }
}

export default defineConfig({plugins: [react(), precompress()], server: {proxy: {'/v1': 'http://localhost:8080', '/web': 'http://localhost:8080'}}, build: {rollupOptions: {input: {app:'index.html', offline:'src/offline/worker.ts'}, output: {entryFileNames: chunk => chunk.name === 'offline' ? 'offline-worker.js' : 'assets/[name]-[hash].js'}}}})
