import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// In development the Go server runs on :8080 and Vite proxies to it.
const backend = process.env.DRAWERED_BACKEND ?? 'http://localhost:8080';

export default defineConfig({
    plugins: [tailwindcss(), sveltekit()],
    server: {
        proxy: {
            '/api': backend,
            '/auth': backend,
            '/files': backend
        }
    },
    build: {
        emptyOutDir: true
    }
});
