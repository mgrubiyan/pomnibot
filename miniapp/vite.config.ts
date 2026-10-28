import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'
import { fileURLToPath } from 'node:url'
import { createAppConfig, parseEnvironment } from './config.ts'

const envDir = fileURLToPath(new URL('..', import.meta.url))

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, envDir, '')
  const environment = parseEnvironment(env.APP_ENV)

  return {
    plugins: [
      react(),
      {
        name: 'max-sdk',
        transformIndexHtml: () => createAppConfig(environment).maxEnabled
          ? [
            { tag: 'script', attrs: { src: 'https://st.max.ru/js/max-web-app.js' }, injectTo: 'head' },
          ]
          : [],
      },
    ],
    envDir,
    define: { __APP_ENV__: JSON.stringify(environment) },
    server: {
      proxy: {
        '/api': {
          target: env.BACKEND_URL || `http://localhost:${env.PORT || '8080'}`,
          changeOrigin: true,
          // Live speech recognition is a WebSocket.
          ws: true,
        },
      },
    },
    optimizeDeps: {
      include: ['@maxhub/max-ui', 'react', 'react-dom', 'react/jsx-runtime'],
    },
  }
})
