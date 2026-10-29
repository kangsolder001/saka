import { describe, it, expect } from 'vitest';
import { queryKeys } from './query-keys';
import { createQueryClient, shouldRetryQuery } from './query-client';
import { ApiError } from './errors';

describe('queryKeys', () => {
	it('defines static keys as const tuples', () => {
		expect(queryKeys.me).toEqual(['me']);
		expect(queryKeys.plans).toEqual(['plans']);
		expect(queryKeys.sessions).toEqual(['me', 'sessions']);
	});

	it('defines dynamic org-scoped keys hierarchically', () => {
		const orgId = 'org-123';
		const baseOrg = queryKeys.organization(orgId);
		expect(baseOrg).toEqual(['orgs', 'org-123']);

		expect(queryKeys.members(orgId)).toEqual(['orgs', 'org-123', 'members']);
		expect(queryKeys.invitations(orgId)).toEqual(['orgs', 'org-123', 'invitations']);
		expect(queryKeys.subscription(orgId)).toEqual(['orgs', 'org-123', 'subscription']);
		expect(queryKeys.invoices(orgId)).toEqual(['orgs', 'org-123', 'invoices']);
		expect(queryKeys.invoice(orgId, 'inv-456')).toEqual(['orgs', 'org-123', 'invoices', 'inv-456']);

		// Hierarchy validation
		expect(queryKeys.members(orgId).slice(0, 2)).toEqual(baseOrg);
		expect(queryKeys.invitations(orgId).slice(0, 2)).toEqual(baseOrg);
		expect(queryKeys.subscription(orgId).slice(0, 2)).toEqual(baseOrg);
		expect(queryKeys.invoices(orgId).slice(0, 2)).toEqual(baseOrg);
		expect(queryKeys.invoice(orgId, 'inv-456').slice(0, 3)).toEqual(queryKeys.invoices(orgId));
	});

	it('defines token-based invitation key', () => {
		expect(queryKeys.invitationByToken('token-abc')).toEqual(['invitation', 'token-abc']);
	});
});

describe('queryClient and retry behavior', () => {
	it('creates client with staleTime of 30,000ms', () => {
		const client = createQueryClient();
		const defaultOptions = client.getDefaultOptions();
		expect(defaultOptions.queries?.staleTime).toBe(30_000);
	});

	it('retries up to 2 times for NETWORK_ERROR', () => {
		const networkError = new ApiError({
			status: 0,
			code: 'NETWORK_ERROR',
			serverMessage: 'Connection failed',
			details: {}
		});

		expect(shouldRetryQuery(0, networkError)).toBe(true);
		expect(shouldRetryQuery(1, networkError)).toBe(true);
		expect(shouldRetryQuery(2, networkError)).toBe(false);
		expect(shouldRetryQuery(3, networkError)).toBe(false);
	});

	it('does NOT retry 4xx client errors', () => {
		const error400 = new ApiError({
			status: 400,
			code: 'VALIDATION_ERROR',
			serverMessage: 'Bad request',
			details: {}
		});
		const error401 = new ApiError({
			status: 401,
			code: 'UNAUTHENTICATED',
			serverMessage: 'Unauthenticated',
			details: {}
		});
		const error403 = new ApiError({
			status: 403,
			code: 'FORBIDDEN',
			serverMessage: 'Forbidden',
			details: {}
		});
		const error404 = new ApiError({
			status: 404,
			code: 'NOT_FOUND',
			serverMessage: 'Not found',
			details: {}
		});
		const error409 = new ApiError({
			status: 409,
			code: 'CONFLICT',
			serverMessage: 'Conflict',
			details: {}
		});
		const error429 = new ApiError({
			status: 429,
			code: 'RATE_LIMITED',
			serverMessage: 'Too many requests',
			details: {}
		});

		expect(shouldRetryQuery(0, error400)).toBe(false);
		expect(shouldRetryQuery(0, error401)).toBe(false);
		expect(shouldRetryQuery(0, error403)).toBe(false);
		expect(shouldRetryQuery(0, error404)).toBe(false);
		expect(shouldRetryQuery(0, error409)).toBe(false);
		expect(shouldRetryQuery(0, error429)).toBe(false);
	});
});
