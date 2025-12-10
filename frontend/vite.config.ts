import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// Function to find the backend API URL
const getApiTarget = (mode: string) => {
    // 1. Check environment variable (VITE_API_TARGET or API_TARGET inside .env)
    const env = loadEnv(mode, process.cwd(), '')
    if (env.API_TARGET) {
        console.log(`Using API_TARGET from env: ${env.API_TARGET}`)
        return env.API_TARGET
    }

    // 2. Try to read from config.json in the parent directory
    try {
        const configPath = path.resolve(__dirname, '../config.json')
        if (fs.existsSync(configPath)) {
            const configData = fs.readFileSync(configPath, 'utf-8')
            const config = JSON.parse(configData)
            if (config.listen_addr) {
                // If it's just a port like ":8080", assume localhost
                let addr = config.listen_addr
                if (addr.startsWith(':')) {
                    addr = `127.0.0.1${addr}`
                }
                const url = `http://${addr}`
                console.log(`Using API URL from config.json: ${url}`)
                return url
            }
        }
    } catch (e) {
        console.warn('Failed to read config.json:', e)
    }

    // 3. Fallback
    console.warn('Using default fallback API URL: http://127.0.0.1:8080')
    return 'http://127.0.0.1:8080'
}

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
    const apiTarget = getApiTarget(mode)

    return {
        plugins: [react()],
        server: {
            proxy: {
                '/api': {
                    target: apiTarget,
                    changeOrigin: true,
                    secure: false, // In case backend uses self-signed HTTPS in future
                }
            }
        }
    }
})
