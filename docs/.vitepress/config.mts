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

// Shared by og: and twitter:, which want the same sentence and would otherwise
// drift apart.
const SITE_URL = 'https://joessst-dev.github.io/fft-cli/'
const SITE_TITLE = 'fft'
const SOCIAL_TITLE = 'fft — one CLI for the fulfillmenttools API'
const SOCIAL_DESCRIPTION =
  "Every one of the fulfillmenttools API's 559 operations in your shell — one binary, one auth path, one output contract. Runs without a tenant."

export default defineConfig({
  title: SITE_TITLE,
  description: 'A command-line client for the fulfillmenttools API.',

  // Project page under joessst-dev.github.io/fft-cli/, not a user/apex site.
  base: '/fft-cli/',

  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: false,

  // The og:/twitter: tags are what a crawler, a Slack unfurl or a search result
  // shows — VitePress's `description` above only reaches <meta name=description>.
  // Only the invariant ones live here; the title and url are per-page and are
  // emitted by transformPageData below, which the description rides along with
  // so a page can override it via frontmatter (none does today). Putting a tag
  // in both places would ship it twice, and a consumer takes the first it sees.
  //
  // No og:image: there is no social card, and pointing one at the favicon.svg
  // renders as a broken tile on every platform that rejects SVG — hence
  // `summary` rather than `summary_large_image`.
  head: [
    ['link', { rel: 'icon', href: '/fft-cli/favicon.svg' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'fft' }],
    ['meta', { name: 'twitter:card', content: 'summary' }],
  ],

  // A deep link is what actually gets shared — `/reference/commands/fft_facility_list`
  // far more often than the home page — so each page unfurls as itself rather than
  // as the site root. The URLs are absolute: a crawler or a chat client resolves
  // them without the page's `base`, so a `/fft-cli/…` path would 404 for everything
  // but a browser already on the site.
  transformPageData(pageData) {
    // cleanUrls is on, so the served path drops `.md` and collapses index files.
    const path = pageData.relativePath
      .replace(/(^|\/)index\.md$/, '$1')
      .replace(/\.md$/, '')
    const isHome = path === ''

    // Mirror VitePress's own createTitle, or og:title contradicts the <title> it
    // shadows: it joins with `|`, and it dedupes when a page's title already *is*
    // the site title — `fft`'s own reference page is <title>fft</title>, not
    // `fft | fft`.
    const title = isHome
      ? SOCIAL_TITLE
      : pageData.title === SITE_TITLE
        ? SITE_TITLE
        : `${pageData.title} | ${SITE_TITLE}`
    const description = pageData.frontmatter.description ?? SOCIAL_DESCRIPTION

    pageData.frontmatter.head ??= []
    pageData.frontmatter.head.push(
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: SITE_URL + path }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
    )
  },

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
            { text: 'Try it without a tenant', link: '/guide/try-offline' },
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
            { text: 'Templates', link: '/guide/templates' },
            { text: 'Read-only projects', link: '/guide/read-only' },
            { text: 'Emulator', link: '/guide/emulator' },
            { text: 'Components', link: '/guide/components' },
            { text: 'AI agents', link: '/guide/agents' },
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

    // A guide page is either generated from a skill asset — which stamps a
    // `source:` into its front matter — or hand-written, in which case the page
    // itself is the source. Sending both to the same file (it used to be the
    // README) means the link is wrong for one of them; ask the page.
    //
    // Reference/commands/* pages are a third kind: `fft gen-docs` stamps only
    // `title:`, so the `source` fallback would otherwise send editors to the
    // drift-gated generated file itself. Point those at the generator instead.
    editLink: {
      pattern: ({ frontmatter, filePath }) =>
        filePath.startsWith('reference/commands/')
          ? 'https://github.com/Joessst-Dev/fft-cli/edit/main/cmd/fft/gendocs.go'
          : `https://github.com/Joessst-Dev/fft-cli/edit/main/${frontmatter.source ?? `docs/${filePath}`}`,
      text: 'Edit this page on GitHub',
    },

    footer: {
      message:
        'An independent open-source project — not affiliated with, endorsed by, or supported by fulfillmenttools.',
      copyright: 'MIT © Joessst-Dev',
    },
  },
})
