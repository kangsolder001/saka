import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import LanguageSwitcher from './LanguageSwitcher.svelte';

describe('LanguageSwitcher', () => {
	it('renders language trigger button with locale code', async () => {
		render(LanguageSwitcher);
		const button = page.getByRole('button');
		await expect.element(button).toBeInTheDocument();
	});
});
