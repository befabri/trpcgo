import { getCollection, type CollectionEntry } from 'astro:content';

const docOrder = [
	'index',
	'concepts',
	'install',
	'quick-start',
	'procedures',
	'router-options',
	'middleware',
	'http-protocol',
	'subscriptions',
	'response-metadata',
	'errors',
	'struct-tags',
	'zod-schemas',
	'code-generation',
	'frontend-setup',
	'security-production',
	'reference/cli',
	'reference/compatibility',
];

export async function renderLlmsFullTxt() {
	const docs = await getCollection('docs', (doc) => !doc.data.draft);
	const docsById = new Map(docs.map((doc) => [doc.id, doc]));
	const orderedDocs = [
		...docOrder.flatMap((id) => {
			const doc = docsById.get(id);
			if (!doc) return [];
			docsById.delete(id);
			return [doc];
		}),
		...Array.from(docsById.values()).sort((a, b) => a.id.localeCompare(b.id)),
	];

	const sections = orderedDocs.map(renderDoc);

	return [`<SYSTEM>This is the full developer documentation for trpcgo.</SYSTEM>`, ...sections].join(
		'\n\n---\n\n'
	);
}

function renderDoc(doc: CollectionEntry<'docs'>) {
	const description = doc.data.description ? `\n\n> ${doc.data.description}` : '';
	const body = normalizeMarkdown(doc.body ?? '');

	return `# ${doc.data.title}${description}\n\n${body}`.trim() + '\n';
}

function normalizeMarkdown(markdown: string) {
	return markdown
		.replace(/^(?:import\s+.*?;\n)+\n*/, '')
		.replace(/^<CardGrid[^>]*>\n?/gm, '')
		.replace(/^<\/CardGrid>\n?/gm, '')
		.replace(/^[\t ]*<Card\s+title="([^"]+)"[^>]*>([\s\S]*?)^[\t ]*<\/Card>/gm, (_match, title, content) => {
			return `### ${title}\n\n${String(content).trim()}\n`;
		})
		.replace(/^:::(note|tip|caution|danger)\n([\s\S]*?)^:::/gm, (_match, variant, content) => {
			const label = String(variant).toUpperCase();
			return `> [!${label}]\n${String(content)
				.trim()
				.split('\n')
				.map((line) => `> ${line}`)
				.join('\n')}`;
		})
		.trim();
}
