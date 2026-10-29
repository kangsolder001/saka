import { describe, it, expect } from 'vitest';
import { ApiError, toApiError } from './errors';

describe('ApiError and toApiError', () => {
	it('handles null response as network error', () => {
		const cause = new Error('Failed to fetch');
		const err = toApiError(null, null, cause);

		expect(err).toBeInstanceOf(ApiError);
		expect(err.status).toBe(0);
		expect(err.code).toBe('NETWORK_ERROR');
		expect(err.serverMessage).toBe('Failed to fetch');
		expect(err.cause).toBe(cause);
		expect(err.retryAfterSeconds).toBeNull();
		expect(err.traceId).toBeNull();
	});

	it('parses standard error response body and traceId', () => {
		const headers = new Headers({
			'X-Trace-ID': 'trace-12345'
		});
		const response = new Response(null, { status: 403, statusText: 'Forbidden', headers });
		const body = {
			error: {
				code: 'FORBIDDEN',
				message: 'Access denied',
				details: {
					reason: 'owner_must_transfer_ownership'
				}
			}
		};

		const err = toApiError(response, body);
		expect(err.status).toBe(403);
		expect(err.code).toBe('FORBIDDEN');
		expect(err.serverMessage).toBe('Access denied');
		expect(err.traceId).toBe('trace-12345');
		expect(err.reason).toBe('owner_must_transfer_ownership');
	});

	it('extracts fieldErrors for VALIDATION_ERROR', () => {
		const response = new Response(null, { status: 400 });
		const body = {
			error: {
				code: 'VALIDATION_ERROR',
				message: 'Validation failed',
				details: {
					fields: {
						email: 'is required',
						password: 'must be at least 8 characters'
					}
				}
			}
		};

		const err = toApiError(response, body);
		expect(err.status).toBe(400);
		expect(err.code).toBe('VALIDATION_ERROR');
		expect(err.fieldErrors).toEqual({
			email: 'is required',
			password: 'must be at least 8 characters'
		});
	});

	it('parses retry-after header and details.retry_after_seconds', () => {
		const headers = new Headers({
			'Retry-After': '120'
		});
		const response = new Response(null, { status: 429, headers });
		const body = {
			error: {
				code: 'RATE_LIMITED',
				message: 'Too many requests',
				details: {
					retry_after_seconds: 60
				}
			}
		};

		// details.retry_after_seconds takes precedence if present
		const err = toApiError(response, body);
		expect(err.retryAfterSeconds).toBe(60);

		// If details doesn't have it, header is used
		const bodyWithoutSeconds = {
			error: {
				code: 'RATE_LIMITED',
				message: 'Rate limited',
				details: {}
			}
		};
		const errFromHeader = toApiError(response, bodyWithoutSeconds);
		expect(errFromHeader.retryAfterSeconds).toBe(120);
	});

	it('handles non-JSON / unexpected body format', () => {
		const headers = new Headers({
			'x-trace-id': 'trace-999'
		});
		const response = new Response(null, { status: 502, statusText: 'Bad Gateway', headers });
		const body = '<html>502 Bad Gateway</html>';

		const err = toApiError(response, body);
		expect(err.status).toBe(502);
		expect(err.code).toBe('INTERNAL_ERROR');
		expect(err.serverMessage).toBe('<html>502 Bad Gateway</html>');
		expect(err.traceId).toBe('trace-999');
		expect(err.reason).toBeNull();
		expect(err.fieldErrors).toEqual({});
	});
});
