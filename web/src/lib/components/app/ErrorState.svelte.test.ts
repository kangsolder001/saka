import { page } from 'vitest/browser';
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ErrorState from './ErrorState.svelte';
import { ApiError } from '$lib/api/errors';

describe('ErrorState', () => {
	it('renders error message, trace ID, and retry button', async () => {
		const retryFn = vi.fn();
		const testError = new ApiError({
			status: 500,
			code: 'INTERNAL_ERROR',
			serverMessage: 'Server crashed',
			traceId: 'tr-abc-999'
		});

		render(ErrorState, {
			error: testError,
			retry: retryFn
		});

		await expect.element(page.getByTestId('error-state')).toBeInTheDocument();
		await expect.element(page.getByTestId('error-trace-id')).toBeInTheDocument();
		await expect.element(page.getByText('tr-abc-999')).toBeInTheDocument();
		await expect.element(page.getByTestId('error-retry-button')).toBeInTheDocument();
	});
});
