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
          ],
        },
        {
          text: 'Project',
          items: [
            { text: 'Comparison vs alternatives', link: '/guide/comparison' },
            { text: 'FAQ', link: '/guide/faq' },
          ],
        },
      ],

      '/cookbook/': [
        {
          text: 'Cookbook',
          items: [
            { text: 'Index', link: '/cookbook/' },
            { text: 'JSON API with SQLite', link: '/cookbook/json-api' },
            { text: 'Multi-tenant by Host header', link: '/cookbook/multi-tenant' },
            { text: 'Device detection (mobile UA)', link: '/cookbook/device-detection' },
            { text: 'CORS for a method-bound route', link: '/cookbook/cors-preflight' },
          ],
        },
      ],

      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Overview', link: '/reference/' },
          ],
        },
      ],

      '/ai/': [
        {
          text: 'AI agents',
          items: [
            { text: 'Overview', link: '/ai/' },
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
})
