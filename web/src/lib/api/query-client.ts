import { QueryClient } from '@tanstack/svelte-query';
import { ApiError } from './errors';

export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
	if (error instanceof ApiError) {
		if (error.code === 'NETWORK_ERROR' || error.status === 0) {
			return failureCount < 2;
		}
		if (error.status >= 400 && error.status < 500) {
			return false;
		}
	}

	if (typeof error === 'object' && error !== null && 'code' in error) {
		if ((error as { code: unknown }).code === 'NETWORK_ERROR') {
			return failureCount < 2;
		}
	}

	return false;
}

export function createQueryClient(): QueryClient {
	return new QueryClient({
		defaultOptions: {
			queries: {
				staleTime: 30_000,
				retry: shouldRetryQuery
			}
		}
	});
}

export const queryClient = createQueryClient();
