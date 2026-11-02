import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from "@tailwindcss/vite";
import path from "path";

// https://vite.dev/config/
export default defineConfig({
    // 使用相对路径：Go 端把 dist 以 /assets/* 静态挂载并提供 SPA 回退，
    // 相对路径可同时兼容根路径与子路径部署。
    base: './',
    plugins: [react(), tailwindcss(),],
    resolve: {
        alias: {
            "@": path.resolve(__dirname, "./src"),
        },
    },
    build: {
        chunkSizeWarningLimit: 900,
    },
    server: {
        port: 5199,
        proxy: {
            '/api': {
                target: 'http://localhost:8080',
                changeOrigin: true,
            },
        },
    },
})
