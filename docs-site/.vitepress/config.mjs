import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: 'Wave',
  description: 'A declarative HTTP server framework — define your backend in YAML, ship a single binary.',
  lang: 'en-US',
  cleanUrls: true,
  lastUpdated: true,

  // GitHub Pages serves at <user>.github.io/wave/, so we need a base.
  // If you later move to a custom domain (wave.dev), change this to '/'.
  base: '/wave/',

  head: [
    ['link', { rel: 'icon', href: '/wave/favicon.svg', type: 'image/svg+xml' }],
    ['meta', { name: 'theme-color', content: '#3eaf7c' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'Wave — declarative HTTP server framework' }],
    ['meta', { property: 'og:description', content: 'Define your backend in YAML, ship a single binary.' }],
  ],

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/quickstart', activeMatch: '/guide/' },
      { text: 'Cookbook', link: '/cookbook/', activeMatch: '/cookbook/' },
      { text: 'Features', link: '/reference/features', activeMatch: '/reference/features' },
      { text: 'Reference', link: '/reference/', activeMatch: '/reference/' },
      { text: 'AI Agents', link: '/ai/', activeMatch: '/ai/' },
      {
        text: 'v0.1.0',
        items: [
          { text: 'CHANGELOG', link: 'https://github.com/luowensheng/wave/blob/main/CHANGELOG.md' },
          { text: 'Releases', link: 'https://github.com/luowensheng/wave/releases' },
        ],
      },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Getting started',
          items: [
            { text: 'What is Wave?', link: '/guide/' },
            { text: 'Quickstart', link: '/guide/quickstart' },
            { text: 'Install', link: '/guide/install' },
            { text: 'Tutorial: build a todo API', link: '/guide/tutorial' },
            { text: 'Wave in your stack', link: '/guide/wave-in-your-stack' },
          ],
        },
        {
          text: 'Concepts',
          items: [
            { text: 'Routes', link: '/guide/concepts-routes' },
            { text: 'Inputs', link: '/guide/concepts-inputs' },
            { text: 'Storage', link: '/guide/concepts-storage' },
            { text: 'Plugins', link: '/guide/concepts-plugins' },
            { text: 'Auth', link: '/guide/concepts-auth' },
            { text: 'Observability', link: '/guide/concepts-observability' },
          ],
        },
        {
          text: 'Deployment',
          items: [
            { text: 'Docker', link: '/guide/deploy-docker' },
            { text: 'Fly.io', link: '/guide/deploy-fly' },
            { text: 'Production checklist', link: '/guide/deploy-checklist' },
          ],
        },
        {
          text: 'Project',
          items: [
            { text: 'Comparison vs alternatives', link: '/guide/comparison' },
            { text: 'FAQ', link: '/guide/faq' },
            { text: 'Privacy', link: '/guide/privacy' },
          ],
        },
      ],

      '/cookbook/': [
        {
          text: 'Backend basics',
          items: [
            { text: 'Index', link: '/cookbook/' },
            { text: 'JSON API with SQLite', link: '/cookbook/json-api' },
            { text: 'File uploads & downloads', link: '/cookbook/file-uploads' },
            { text: 'Rate-limit an endpoint', link: '/cookbook/rate-limit' },
            { text: 'Functional testing (`wave test`)', link: '/cookbook/testing' },
          ],
        },
        {
          text: 'Auth',
          items: [
            { text: 'Magic-link login', link: '/cookbook/magic-link-login' },
            { text: 'OAuth (Google/GitHub/Apple)', link: '/cookbook/oauth' },
            { text: 'Audit log every mutation', link: '/cookbook/audit-log' },
          ],
        },
        {
          text: 'Routing',
          items: [
            { text: 'Multi-tenant by Host header', link: '/cookbook/multi-tenant' },
            { text: 'Device detection (mobile UA)', link: '/cookbook/device-detection' },
            { text: 'A/B testing via cookie', link: '/cookbook/ab-testing' },
            { text: 'CORS for a method-bound route', link: '/cookbook/cors-preflight' },
          ],
        },
        {
          text: 'Streaming & jobs',
          items: [
            { text: 'Stream events with SSE', link: '/cookbook/sse' },
            { text: 'Background tasks', link: '/cookbook/background-tasks' },
            { text: 'Schedule a cron job', link: '/cookbook/schedule' },
          ],
        },
        {
          text: 'Integrations',
          items: [
            { text: 'Forward Stripe webhooks', link: '/cookbook/stripe-webhooks' },
            { text: 'Outbox-backed delivery', link: '/cookbook/outbox' },
          ],
        },
        {
          text: 'Use Wave with your existing stack',
          items: [
            { text: 'React / Next.js + Wave backend', link: '/cookbook/react-wave' },
            { text: 'Wave in front of a Node service', link: '/cookbook/node-gateway' },
            { text: 'Wave as a Python sidekick', link: '/cookbook/python-sidekick' },
            { text: 'Migrating from Express incrementally', link: '/cookbook/migrate-from-express' },
          ],
        },
      ],

      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Overview', link: '/reference/' },
            { text: 'Full feature inventory', link: '/reference/features' },
          ],
        },
      ],

      '/ai/': [
        {
          text: 'AI agents',
          items: [
            { text: 'Overview', link: '/ai/' },
            { text: 'Token efficiency', link: '/ai/token-efficiency' },
            { text: 'Claude Code skill', link: '/ai/claude-code' },
            { text: 'Cursor + editors', link: '/ai/editors' },
            { text: 'Prompt patterns', link: '/ai/prompts' },
            { text: 'llms.txt', link: '/ai/llms-txt' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/luowensheng/wave' },
    ],

    editLink: {
      pattern: 'https://github.com/luowensheng/wave/edit/main/docs-site/:path',
      text: 'Edit this page on GitHub',
    },

    footer: {
      message: 'Released under the Apache-2.0 License.',
      copyright: 'Copyright © 2026 The Wave Authors',
    },

    search: { provider: 'local' },
  },

  // Wave docs contain many `{{name}}` SQL-template fragments inline,
  // which VitePress would otherwise try to evaluate as Vue mustaches.
  // Wrap every inline-code token in <span v-pre> so Vue skips it.
  // Fenced code blocks (```) are already safe — this only fixes
  // single-backtick inline code.
  markdown: {
    config: (md) => {
      const defaultInlineCode = md.renderer.rules.code_inline
        || ((tokens, idx, opts, env, self) => self.renderToken(tokens, idx, opts))
      md.renderer.rules.code_inline = (tokens, idx, opts, env, self) => {
        const rendered = defaultInlineCode(tokens, idx, opts, env, self)
        return `<span v-pre>${rendered}</span>`
      }
    },
  },
})
