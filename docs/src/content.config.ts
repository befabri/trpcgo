import { defineCollection } from 'astro:content';
import { docsLoader, i18nLoader } from '@astrojs/starlight/loaders';
import { docsSchema, i18nSchema } from '@astrojs/starlight/schema';

export const collections = {
	docs: defineCollection({ loader: docsLoader(), schema: docsSchema() }),
	// Starlight always reads the i18n collection; defining it keeps Astro from
	// warning about a missing collection on this single-language site.
	i18n: defineCollection({ loader: i18nLoader(), schema: i18nSchema() }),
};
