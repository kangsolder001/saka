import type { ErrorCode } from './types';

export type ApiErrorCode = ErrorCode | 'NETWORK_ERROR';

export class ApiError extends Error {
	status: number;
	code: ApiErrorCode;
	serverMessage: string;
	details: Record<string, unknown>;
	retryAfterSeconds: number | null;
	traceId: string | null;

	constructor(params: {
		status: number;
		code: ApiErrorCode;
		serverMessage: string;
		details?: Record<string, unknown>;
		retryAfterSeconds?: number | null;
		traceId?: string | null;
		cause?: unknown;
	}) {
		super(params.serverMessage, { cause: params.cause });
		this.name = 'ApiError';
		this.status = params.status;
		this.code = params.code;
		this.serverMessage = params.serverMessage;
		this.details = params.details ?? {};
		this.retryAfterSeconds = params.retryAfterSeconds ?? null;
		this.traceId = params.traceId ?? null;
	}

	get reason(): string | null {
		return typeof this.details.reason === 'string' ? this.details.reason : null;
	}

	get fieldErrors(): Record<string, string> {
		if (
			this.code === 'VALIDATION_ERROR' &&
			typeof this.details.fields === 'object' &&
			this.details.fields !== null
		) {
			return this.details.fields as Record<string, string>;
		}
		return {};
	}
}

export function toApiError(response: Response | null, body: unknown, cause?: unknown): ApiError {
	if (!response) {
		const message = cause instanceof Error ? cause.message : 'Network error';
		return new ApiError({
			status: 0,
			code: 'NETWORK_ERROR',
			serverMessage: message,
			details: {},
			retryAfterSeconds: null,
			traceId: null,
			cause
		});
	}

	const traceId = response.headers.get('x-trace-id') || response.headers.get('X-Trace-ID') || null;

	let retryAfterSeconds: number | null = null;
	const retryAfterHeader =
		response.headers.get('retry-after') || response.headers.get('Retry-After');
	if (retryAfterHeader) {
		const parsed = parseInt(retryAfterHeader, 10);
		if (!isNaN(parsed) && parsed >= 0) {
			retryAfterSeconds = parsed;
		} else {
			const date = Date.parse(retryAfterHeader);
			if (!isNaN(date)) {
				retryAfterSeconds = Math.max(0, Math.ceil((date - Date.now()) / 1000));
			}
		}
	}

	if (
		typeof body === 'object' &&
		body !== null &&
		'error' in body &&
		typeof (body as { error: unknown }).error === 'object' &&
		(body as { error: unknown }).error !== null
	) {
		const errObj = (body as { error: { code?: unknown; message?: unknown; details?: unknown } })
			.error;
		const code = typeof errObj.code === 'string' ? (errObj.code as ErrorCode) : 'INTERNAL_ERROR';
		const serverMessage =
			typeof errObj.message === 'string' ? errObj.message : response.statusText || 'Error';
		const details =
			typeof errObj.details === 'object' && errObj.details !== null
				? (errObj.details as Record<string, unknown>)
				: {};

		if (typeof details.retry_after_seconds === 'number') {
			retryAfterSeconds = details.retry_after_seconds;
		}

		return new ApiError({
			status: response.status,
			code,
			serverMessage,
			details,
			retryAfterSeconds,
			traceId,
			cause
		});
	}

	return new ApiError({
		status: response.status,
		code: 'INTERNAL_ERROR',
		serverMessage:
			typeof body === 'string' && body.trim() ? body : response.statusText || 'Internal Error',
		details: {},
		retryAfterSeconds,
		traceId,
		cause
	});
}
