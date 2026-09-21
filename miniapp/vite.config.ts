import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  optimizeDeps: {
    include: ['@maxhub/max-ui', 'react', 'react-dom', 'react/jsx-runtime'],
  },
})