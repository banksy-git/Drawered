import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
    preprocess: vitePreprocess(),
    kit: {
        // The Go server embeds this directory and serves index.html for
        // any client-side route.
        adapter: adapter({
            pages: '../internal/webui/dist',
            assets: '../internal/webui/dist',
            fallback: 'index.html',
            strict: false
        })
    }
};

export default config;
