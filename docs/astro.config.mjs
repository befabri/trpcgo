// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

const site = process.env.DOCS_SITE || 'https://trpcgo.dev';

// https://astro.build/config
export default defineConfig({
	site,
	integrations: [
		starlight({
			title: 'trpcgo',
			description: 'Build Go APIs for TypeScript tRPC clients.',
			favicon: '/favicon.svg',
			logo: {
				src: './src/assets/logo.svg',
				alt: '',
				replacesTitle: true,
			},
			customCss: ['./src/styles/copy-page-markdown.css'],
			components: {
				Head: './src/components/Head.astro',
				PageTitle: './src/components/PageTitleWithCopy.astro',
			},
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/befabri/trpcgo' }],
			sidebar: [
				{
					label: 'Start',
					items: [
						{ label: 'Overview', link: '/' },
						{ label: 'Core Concepts', slug: 'concepts' },
						{ label: 'Install', slug: 'install' },
						{ label: 'Quick Start', slug: 'quick-start' },
						{ label: 'llms.txt', link: '/llms.txt' },
						{ label: 'llms-full.txt', link: '/llms-full.txt' },
					],
				},
				{
					label: 'Runtime',
					items: [
						{ label: 'Procedures', slug: 'procedures' },
						{ label: 'Router & Options', slug: 'router-options' },
						{ label: 'Middleware & Metadata', slug: 'middleware' },
						{ label: 'HTTP Protocol', slug: 'http-protocol' },
						{ label: 'Subscriptions', slug: 'subscriptions' },
						{ label: 'Response Metadata', slug: 'response-metadata' },
						{ label: 'Errors', slug: 'errors' },
					],
				},
				{
					label: 'Type Generation',
					items: [
						{ label: 'Struct Tags', slug: 'struct-tags' },
						{ label: 'Zod Schemas', slug: 'zod-schemas' },
						{ label: 'Code Generation', slug: 'code-generation' },
						{ label: 'Frontend Setup', slug: 'frontend-setup' },
					],
				},
				{
					label: 'Operations',
					items: [{ label: 'Security & Production', slug: 'security-production' }],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'CLI', slug: 'reference/cli' },
						{ label: 'Compatibility', slug: 'reference/compatibility' },
					],
				},
			],
		}),
	],
});
