import type { APIRoute } from 'astro';
import { renderLlmsTxt } from '../llms-txt.js';

export const prerender = true;

export const GET: APIRoute = ({ site }) => {
	return new Response(renderLlmsTxt(site ?? 'https://trpcgo.dev'), {
		headers: {
			'Content-Type': 'text/plain; charset=utf-8',
		},
	});
};
