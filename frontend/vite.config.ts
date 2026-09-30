import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
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
