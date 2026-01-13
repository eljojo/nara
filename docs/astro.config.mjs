import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  base: '/docs',
  output: 'static',
  integrations: [
    starlight({
      title: 'Nara Docs',
      description: 'Field guide and reference for the Nara network.',
      social: {
        github: 'https://github.com/eljojo/nara'
      },
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
      ]
    })
  ]
});
