// @ts-check
import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import preact from '@astrojs/preact';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';

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
            { label: 'The Network', link: '/' }
          ]
        },
        {
          label: 'Running Nara',
          items: [
            { label: 'How to Deploy', link: '/running-nara/how-to-deploy/' },
          ]
        },
        {
          label: 'Specification',
          items: [
            { label: 'Living Spec Home', link: '/spec/' },
            { label: 'Overview & Philosophy', link: '/spec/overview/' },
            { label: 'Identity', link: '/spec/identity/' },
            { label: 'Personality', link: '/spec/personality/' },
            { label: 'Aura & Avatar', link: '/spec/aura-and-avatar/' },
            { label: 'Events', link: '/spec/events/' },
            { label: 'Projections', link: '/spec/projections/' },
            { label: 'Memory Model', link: '/spec/memory-model/' },
            { label: 'Plaza MQTT', link: '/spec/plaza-mqtt/' },
            { label: 'Mesh HTTP', link: '/spec/mesh-http/' },
            { label: 'Zines', link: '/spec/zines/' },
            { label: 'Sync Protocol', link: '/spec/sync-protocol/' },
            { label: 'Presence', link: '/spec/presence/' },
            { label: 'Observations', link: '/spec/observations/' },
            { label: 'Checkpoints', link: '/spec/checkpoints/' },
            { label: 'Stash', link: '/spec/stash/' },
            { label: 'Social Events', link: '/spec/social-events/' },
            { label: 'Clout', link: '/spec/clout/' },
            { label: 'World Postcards', link: '/spec/world-postcards/' },
            { label: 'Coordinates (Spec)', link: '/spec/coordinates/' },
            { label: 'HTTP API', link: '/spec/http-api/' },
            { label: 'Web UI', link: '/spec/web-ui/' },
            { label: 'Boot Sequence', link: '/spec/boot-sequence/' },
            { label: 'Configuration', link: '/spec/configuration/' },
            { label: 'Deployment', link: '/spec/deployment/' },
            { label: 'Styleguide', link: '/spec/styleguide/' }
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
    mermaid({
      autoTheme: true
    })
	],
  vite: {
    server: {
      fs: {
        allow: ['..'],
      },
    },
  },
});
