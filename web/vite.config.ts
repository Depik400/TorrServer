import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import { nodePolyfills } from 'vite-plugin-node-polyfills'

import path from 'path'

export default defineConfig(({ command, mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    plugins: [react(), nodePolyfills({ include: ['buffer', 'fs'] })],
    build: {
      outDir: 'build',
      assetsDir: 'static',
      emptyOutDir: true,
    },
    server: {
      proxy: {
        '/api': {
          target: env.VITE_SERVER_HOST,
          changeOrigin: true,
        },
      },
    },
    resolve: {
      alias: {
        style: path.resolve(__dirname, 'src/style'),
        components: path.resolve(__dirname, 'src/components'),
        utils: path.resolve(__dirname, 'src/utils'),
        icons: path.resolve(__dirname, 'src/icons'),
        '@': path.resolve(__dirname, 'src'),
      },
    },
  }
})
