import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ThemeSwitcher from './ThemeSwitcher.svelte';

describe('ThemeSwitcher', () => {
	it('renders theme trigger button', async () => {
		render(ThemeSwitcher);
		const button = page.getByRole('button');
		await expect.element(button).toBeInTheDocument();
	});
});
