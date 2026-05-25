import TurndownService from 'turndown';
import { gfm } from 'turndown-plugin-gfm';

const markdownConverter = createMarkdownConverter();

declare global {
	interface Window {
		__trpcgoCopyPageMarkdownInitialized?: boolean;
	}
}

export function setupCopyPageMarkdown(): void {
	if (window.__trpcgoCopyPageMarkdownInitialized) {
		mountCopyButtons();
		return;
	}

	window.__trpcgoCopyPageMarkdownInitialized = true;
	document.addEventListener('astro:page-load', mountCopyButtons);
	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', mountCopyButtons, { once: true });
	} else {
		mountCopyButtons();
	}
}

function mountCopyButtons(): void {
	document
		.querySelectorAll<HTMLButtonElement>(
			'[data-copy-page-markdown-button]:not([data-copy-page-markdown-ready])'
		)
		.forEach((button) => {
			button.dataset.copyPageMarkdownReady = 'true';
			button.addEventListener('click', () => copyCurrentPage(button));
		});
}

async function copyCurrentPage(button: HTMLButtonElement): Promise<void> {
	const row = button.closest<HTMLElement>('[data-copy-page-markdown-row]');
	const liveRegion = row?.querySelector<HTMLElement>('[data-copy-page-markdown-live]');

	button.disabled = true;
	try {
		await writeClipboardText(getCopyValue(row));
		showFeedback(liveRegion, 'Copied', false);
	} catch {
		showFeedback(liveRegion, 'Copy failed', true);
	} finally {
		window.setTimeout(() => {
			button.disabled = false;
		}, 300);
	}
}

function getCopyValue(row: HTMLElement | null): string {
	const markdownContent = document.querySelector<HTMLElement>('main .sl-markdown-content');
	if (!markdownContent) {
		throw new Error('Markdown content root was not found.');
	}

	const clone = markdownContent.cloneNode(true) as HTMLElement;
	sanitizeClone(clone);
	const markdownBody = markdownConverter.turndown(clone).trim();
	const title = row?.querySelector<HTMLHeadingElement>('h1#_top');
	const titleText = normalizeWhitespace(title?.textContent?.trim() || document.title || 'Untitled');
	const sections = [`# ${titleText}`];
	if (markdownBody) {
		sections.push(markdownBody);
	}
	return sections.join('\n\n').trim() + '\n';
}

function sanitizeClone(root: HTMLElement): void {
	for (const selector of [
		'script',
		'style',
		'noscript',
		'.sl-anchor-link',
		'.expressive-code .copy',
		'[data-pagefind-ignore]',
		'[data-copy-page-markdown-row]',
	]) {
		root.querySelectorAll(selector).forEach((node) => node.remove());
	}
}

function createMarkdownConverter(): TurndownService {
	const turndown = new TurndownService({
		headingStyle: 'atx',
		codeBlockStyle: 'fenced',
		bulletListMarker: '-',
		emDelimiter: '*',
		strongDelimiter: '**',
	});

	turndown.use(gfm);

	turndown.addRule('expressiveCodeBlock', {
		filter: (node) => isElement(node) && node.classList.contains('expressive-code'),
		replacement(_content, node) {
			if (!isElement(node)) {
				return '\n\n';
			}

			const code = node.querySelector('pre code');
			if (!code) {
				return '\n\n';
			}

			const pre = code.closest('pre');
			const language =
				pre?.getAttribute('data-language') ||
				code.getAttribute('data-language') ||
				code.className.match(/language-([\w-]+)/)?.[1] ||
				'';
			const rawCode = normalizeNewlines(getCodeText(code)).replace(/\n$/, '');
			const fence = getFence(rawCode);
			const openingFence = language ? `${fence}${language}` : fence;

			return `\n\n${openingFence}\n${rawCode}\n${fence}\n\n`;
		},
	});

	return turndown;
}

function showFeedback(region: HTMLElement | null | undefined, text: string, isError: boolean): void {
	if (!region) {
		return;
	}
	region.textContent = '';

	const feedback = document.createElement('div');
	feedback.className = 'copy-page-markdown-feedback';
	if (isError) {
		feedback.classList.add('copy-page-markdown-feedback--error');
	}
	feedback.textContent = text;
	region.append(feedback);

	requestAnimationFrame(() => feedback.classList.add('show'));
	window.setTimeout(() => feedback.classList.remove('show'), 1800);
	window.setTimeout(() => feedback.remove(), 2400);
}

function isElement(node: Node): node is HTMLElement {
	return node.nodeType === Node.ELEMENT_NODE;
}

function getFence(code: string): string {
	const maxBackticks = Math.max(...(code.match(/`+/g) || ['']).map((value) => value.length));
	return '`'.repeat(Math.max(3, maxBackticks + 1));
}

function normalizeWhitespace(text: string): string {
	return text.replace(/\s+/g, ' ').trim();
}

function normalizeNewlines(text: string): string {
	return text.replace(/\r\n?/g, '\n');
}

function getCodeText(code: Element): string {
	const expressiveCodeLines = Array.from(code.querySelectorAll<HTMLElement>('.ec-line'));
	if (expressiveCodeLines.length === 0) {
		return code.textContent || '';
	}

	return expressiveCodeLines
		.map((line) => line.querySelector<HTMLElement>('.code')?.textContent || '')
		.join('\n');
}

async function writeClipboardText(text: string): Promise<void> {
	if (navigator.clipboard?.writeText && window.isSecureContext) {
		await navigator.clipboard.writeText(text);
		return;
	}

	const helper = document.createElement('pre');
	Object.assign(helper.style, {
		height: '1px',
		left: '0',
		overflow: 'hidden',
		pointerEvents: 'none',
		position: 'fixed',
		top: '0',
		userSelect: 'all',
		width: '1px',
	});
	helper.ariaHidden = 'true';
	helper.textContent = text;
	document.body.append(helper);

	const range = document.createRange();
	range.selectNodeContents(helper);
	const selection = getSelection();
	if (!selection) {
		helper.remove();
		throw new Error('Selection API unavailable.');
	}

	selection.removeAllRanges();
	selection.addRange(range);

	let didCopy = false;
	try {
		didCopy = document.execCommand('copy');
	} finally {
		selection.removeAllRanges();
		helper.remove();
	}
	if (!didCopy) {
		throw new Error('Copy command failed.');
	}
}
