import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PageSkeleton from './PageSkeleton.svelte';

describe('PageSkeleton', () => {
	it('renders skeleton loading placeholders', async () => {
		render(PageSkeleton);

		await expect.element(page.getByTestId('page-skeleton')).toBeInTheDocument();
	});
});
