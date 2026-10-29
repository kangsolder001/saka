import { page } from 'vitest/browser';
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import LoadMore from './LoadMore.svelte';

describe('LoadMore', () => {
	it('renders load more button when hasMore is true', async () => {
		const onLoadMore = vi.fn();
		render(LoadMore, {
			hasMore: true,
			loading: false,
			onLoadMore
		});

		await expect.element(page.getByTestId('load-more-button')).toBeInTheDocument();
	});

	it('does not render when hasMore is false', async () => {
		const onLoadMore = vi.fn();
		render(LoadMore, {
			hasMore: false,
			loading: false,
			onLoadMore
		});

		await expect.element(page.getByTestId('load-more-button')).not.toBeInTheDocument();
	});
});
