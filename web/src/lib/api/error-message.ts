import * as m from '#lib/paraglide/messages.js';
import { getLocale } from '#lib/paraglide/runtime.js';
import { ApiError } from './errors';

function formatRetryAfter(seconds: number): string {
	const locale = getLocale();
	if (seconds > 60) {
		const mins = Math.ceil(seconds / 60);
		return locale === 'id' ? `${mins} menit` : `${mins} minute${mins > 1 ? 's' : ''}`;
	}
	return locale === 'id' ? `${seconds} detik` : `${seconds} second${seconds !== 1 ? 's' : ''}`;
}

export function errorMessage(err: unknown): string {
	if (!(err instanceof ApiError)) {
		if (err instanceof Error && err.message) {
			return err.message;
		}
		return m.error_internal_error();
	}

	// 1. Check reason if present
	if (err.reason) {
		const reasonKey = `error_reason_${err.reason}` as keyof typeof m;
		const reasonFn = m[reasonKey];
		if (typeof reasonFn === 'function') {
			return (reasonFn as () => string)();
		}
	}

	// 2. Rate limited special formatting if retryAfterSeconds present
	if (err.code === 'RATE_LIMITED' && err.retryAfterSeconds !== null) {
		const timeStr = formatRetryAfter(err.retryAfterSeconds);
		return m.error_rate_limited({ seconds: timeStr });
	}

	// 3. Check code
	const codeKey = `error_${err.code.toLowerCase()}` as keyof typeof m;
	const codeFn = m[codeKey];
	if (typeof codeFn === 'function') {
		if (err.code === 'RATE_LIMITED') {
			return (codeFn as ({ seconds }: { seconds: string }) => string)({
				seconds: getLocale() === 'id' ? 'beberapa saat' : 'a moment'
			});
		}
		return (codeFn as () => string)();
	}

	// 4. Fallback to serverMessage or internal_error
	if (err.serverMessage && err.serverMessage.trim() !== '') {
		return err.serverMessage;
	}

	return m.error_internal_error();
}
