import * as m from '#lib/paraglide/messages.js';

export function fieldErrorMessage(field: string, reason: string): string {
	const trimmedReason = reason.trim().toLowerCase();

	if (trimmedReason === 'is required' || trimmedReason === 'required') {
		return m.field_error_required();
	}

	if (field === 'current_password' && trimmedReason === 'is incorrect') {
		return m.field_error_current_password_incorrect();
	}

	if (
		field === 'new_password' &&
		(trimmedReason === 'must be different from the current password' ||
			trimmedReason === 'must be different from current password')
	) {
		return m.field_error_new_password_same();
	}

	if (
		field === 'new_email' &&
		(trimmedReason === 'must be different from the current email' ||
			trimmedReason === 'must be different from current email')
	) {
		return m.field_error_new_email_same();
	}

	if (
		field === 'token' &&
		(trimmedReason === 'invalid or expired' || trimmedReason === 'is invalid or expired')
	) {
		return m.field_error_token_invalid();
	}

	if (field === 'user_id' && trimmedReason === 'must be another member') {
		return m.field_error_user_id_another_member();
	}

	if (
		field === 'plan' &&
		(trimmedReason === 'is not a known plan' || trimmedReason === 'unknown plan')
	) {
		return m.field_error_plan_unknown();
	}

	return m.field_error_invalid();
}
