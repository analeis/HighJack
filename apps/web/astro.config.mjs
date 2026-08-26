// @ts-check
import { defineConfig } from 'astro/config';
import solid from '@astrojs/solid-js';
import tailwindcss from '@tailwindcss/vite';

// The website is statically generated and deployed independently of the
// game server (Cloudflare Pages; see infra/cloudflare/).
export default defineConfig({
  integrations: [solid()],
  vite: {
    plugins: [tailwindcss()],
  },
});
