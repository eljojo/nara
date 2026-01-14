// @ts-check
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import preact from '@astrojs/preact';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
  base: '/docs',
  output: 'static',
	integrations: [
		starlight({
      title: 'nara',
      description: 'Field guide and reference for the Nara network.',
			social: [
        { icon: 'external', label: 'Network', href: 'https://nara.network' },
        { icon: 'github', label: 'GitHub', href: 'https://github.com/eljojo/nara' }
      ],
			sidebar: [
        {
          label: 'Overview',
          items: [
            { label: 'Welcome', link: '/' }
          ]
        },
        {
          label: 'Concepts',
          items: [
            { label: 'Events', link: '/concepts/events/' },
            { label: 'Sync', link: '/concepts/sync/' },
            { label: 'Observations', link: '/concepts/observations/' },
            { label: 'Stash', link: '/concepts/stash/' },
            { label: 'World Journeys', link: '/concepts/world/' },
            { label: 'Coordinates', link: '/concepts/coordinates/' }
          ]
        }
			],
		}),
    mdx(),
    preact(),
	],
  vite: {
    server: {
      fs: {
        allow: ['..'],
      },
    },
  },
});
