import { defineConfig } from 'vitepress'
import { readdirSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'

// The CLI reference is generated (make docs → fft gen-docs), so its sidebar is
// built from whatever pages exist on disk rather than a list kept in sync by hand.
// A page named fft_facility_list.md is the command `fft facility list`.
function referenceSidebar() {
  const dir = fileURLToPath(new URL('../reference/commands', import.meta.url))
  const pages = readdirSync(dir)
    .filter((f) => f.endsWith('.md'))
    .map((f) => f.replace(/\.md$/, ''))
    .sort()

  return pages.map((slug) => ({
    text: slug.replace(/_/g, ' '),
    link: `/reference/commands/${slug}`,
  }))
}

export default defineConfig({
  title: 'fft',
  description: 'A command-line client for the fulfillmenttools API.',

  // Project page under joessst-dev.github.io/fft-cli/, not a user/apex site.
  base: '/fft-cli/',

  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: false,

  head: [['link', { rel: 'icon', href: '/fft-cli/favicon.svg' }]],

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/install' },
      { text: 'Commands', link: '/guide/commands' },
      { text: 'CLI reference', link: '/reference/' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Getting started',
          items: [
            { text: 'Install', link: '/guide/install' },
            { text: 'Before you begin', link: '/guide/prerequisites' },
            { text: 'Getting started', link: '/guide/getting-started' },
            { text: 'Setting up a project', link: '/guide/configuration' },
            { text: 'Authentication', link: '/guide/auth' },
            { text: 'CI & headless use', link: '/guide/ci' },
          ],
        },
        {
          text: 'Using fft',
          items: [
            { text: 'Overview', link: '/guide/overview' },
            { text: 'Commands', link: '/guide/commands' },
            { text: 'Discovery', link: '/guide/discovery' },
            { text: 'Recipes', link: '/guide/recipes' },
            { text: 'Emulator', link: '/guide/emulator' },
            { text: 'Troubleshooting', link: '/guide/troubleshooting' },
          ],
        },
      ],
      '/reference/': [
        { text: 'CLI reference', link: '/reference/' },
        { text: 'Commands', items: referenceSidebar() },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/Joessst-Dev/fft-cli' },
    ],

    search: { provider: 'local' },

    editLink: {
      pattern: 'https://github.com/Joessst-Dev/fft-cli/edit/main/README.md',
      text: 'These pages are generated — edit the source',
    },

    footer: {
      message:
        'An independent open-source project — not affiliated with, endorsed by, or supported by fulfillmenttools.',
      copyright: 'MIT © Jost Weyers',
    },
  },
})
