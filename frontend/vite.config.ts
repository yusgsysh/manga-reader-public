import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { defineConfig } from 'vitest/config'
import type { PluginOption, ResolvedConfig } from 'vite'

/**
 * Desktop dev only: the Wails shell starts Gin on a random 127.0.0.1 port and
 * records the address in a small JSON file. Publish it to the page as
 * `window.__MANGA_READER_CONFIG__` so the UI can call the API directly — the
 * backend already answers CORS preflights with `Access-Control-Allow-Origin: *`.
 * Production desktop builds receive the same value from Go, injected into the
 * embedded index.html instead.
 */
function mangaReaderRuntimeConfig(): PluginOption {
  let desktopMode = false

  return {
    name: 'manga-reader:runtime-config',
    apply: 'serve',
    configResolved(config: ResolvedConfig) {
      desktopMode = config.mode === 'desktop'
    },
    transformIndexHtml() {
      if (!desktopMode) return undefined
      return [
        {
          tag: 'script',
          injectTo: 'head-prepend',
          children: `window.__MANGA_READER_CONFIG__=${JSON.stringify({
            apiBaseUrl: readRuntimeApiBaseURL(),
          })};`,
        },
      ]
    },
  }
}

/** Must mirror `runtimeConfigPath()` in backend/cmd/desktop. */
function readRuntimeApiBaseURL(): string {
  const file =
    process.env.MANGA_READER_RUNTIME_FILE ??
    join(tmpdir(), 'manga-reader-desktop.json')
  try {
    const parsed = JSON.parse(readFileSync(file, 'utf8')) as {
      apiBaseUrl?: unknown
    }
    return typeof parsed.apiBaseUrl === 'string' ? parsed.apiBaseUrl : ''
  } catch {
    return ''
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss(), mangaReaderRuntimeConfig()],
  build: {
    // The kumo + motion + router + query vendor bundle is intentionally shared;
    // silence the default 500 kB warning rather than over-splitting.
    chunkSizeWarningLimit: 800,
  },
  test: {
    globals: true,
    environment: 'node',
  },
})
