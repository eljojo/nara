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
          label: 'Features',
          items: [
            { label: 'Zines', link: '/spec/features/zines/' },
            { label: 'Stash', link: '/spec/features/stash/' },
            { label: 'World Postcards', link: '/spec/features/world-postcards/' },
            { label: 'Web UI', link: '/spec/features/web-ui/' }
          ]
        },
        {
          label: 'Specification',
          items: [
            { label: 'Living Spec Home', link: '/spec/' },
            { label: 'Overview & Philosophy', link: '/spec/overview/' },
            { label: 'Styleguide', link: '/spec/styleguide/' },
            {
              label: 'Identity & Being',
              items: [
                { label: 'Identity', link: '/spec/runtime/identity/' },
                { label: 'Personality', link: '/spec/personality/' },
                { label: 'Aura & Avatar', link: '/spec/aura-and-avatar/' }
              ]
            },
            {
              label: 'Runtime Architecture',
              items: [
                { label: 'Runtime & Primitives', link: '/spec/runtime/runtime/' },
                { label: 'Pipelines & Stages', link: '/spec/runtime/pipelines/' },
                { label: 'Behaviours & Patterns', link: '/spec/runtime/behaviours/' }
              ]
            },
            {
              label: 'Event & Memory',
              items: [
                { label: 'Events', link: '/spec/developer/events/' },
                { label: 'Projections', link: '/spec/projections/' },
                { label: 'Memory Model', link: '/spec/memory-model/' }
              ]
            },
            {
              label: 'Transport & Sync',
              items: [
                { label: 'Plaza MQTT', link: '/spec/developer/plaza-mqtt/' },
                { label: 'Mesh HTTP', link: '/spec/developer/mesh-http/' },
                { label: 'Sync Protocol', link: '/spec/developer/sync/' }
              ]
            },

            {
              label: 'Services',
              items: [
                { label: 'Presence', link: '/spec/presence/' },
                { label: 'Observations', link: '/spec/services/observations/' },
                { label: 'Checkpoints', link: '/spec/services/checkpoints/' },
                { label: 'Social Events', link: '/spec/services/social/' },
                { label: 'Clout', link: '/spec/clout/' },
                { label: 'Network Coordinates', link: '/spec/services/coordinates/' }
              ]
            },
            {
              label: 'Operations & UI',
              items: [
                { label: 'HTTP API', link: '/spec/http-api/' },
                { label: 'Boot Sequence', link: '/spec/boot-sequence/' },
                { label: 'Configuration', link: '/spec/configuration/' },
                { label: 'Deployment', link: '/spec/deployment/' }
              ]
            }
          ]
        },
        {
          label: 'Developer Section',
          items: [
            { label: 'Developer Guide', link: '/spec/developer-guide/' },
            { label: 'Events Reference', link: '/spec/developer/events/' },
            { label: 'Sync Protocol', link: '/spec/developer/sync/' },
            { label: 'Plaza MQTT', link: '/spec/developer/plaza-mqtt/' },
            { label: 'Mesh HTTP', link: '/spec/developer/mesh-http/' },
            { label: 'Cryptography (Keyring)', link: '/spec/developer/cryptography/' },
            { label: 'Mesh Client', link: '/spec/developer/mesh-client/' },
            { label: 'Sample Service (Stash)', link: '/spec/developer/sample-service/' },

            {
              label: 'API Reference',
              items: [
                { label: 'Identity', link: '/api/identity/' },
                { label: 'Messages', link: '/api/messages/' },
                { label: 'Runtime', link: '/api/runtime/' },
                { label: 'Utilities', link: '/api/utilities/' },
                { label: 'Types', link: '/api/types/' },
                { label: 'Stash Service', link: '/api/stash/' }
              ]
            }
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
