import type { APIRoute } from 'astro';
import { renderLlmsFullTxt } from '../llms-full-txt';

export const prerender = true;

export const GET: APIRoute = async () => {
	return new Response(await renderLlmsFullTxt(), {
		headers: {
			'Content-Type': 'text/plain; charset=utf-8',
		},
	});
};
