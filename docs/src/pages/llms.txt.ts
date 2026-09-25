import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';
import { renderLlmsTxt } from '../llms-txt.js';

export const prerender = true;

export const GET: APIRoute = async ({ site }) => {
	const body = renderLlmsTxt(site ?? 'https://trpcgo.dev');

	const docs = await getCollection('docs', (doc) => !doc.data.draft);
	const missing = docs.filter((doc) => !body.includes(`/${doc.id}/)`)).map((doc) => doc.id);
	if (missing.length > 0) {
		throw new Error(`src/llms-txt.js does not link ${missing.join(', ')}`);
	}

	return new Response(body, {
		headers: {
			'Content-Type': 'text/plain; charset=utf-8',
		},
	});
};
