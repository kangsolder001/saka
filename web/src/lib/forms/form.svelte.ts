import type { ZodSchema } from 'zod';
import { ApiError } from '$lib/api/errors';
import { errorMessage } from '$lib/api/error-message';
import { fieldErrorMessage } from './field-errors';

export interface CreateFormOptions<T extends Record<string, unknown>> {
	schema: ZodSchema<T>;
	initial: T;
	onSubmit: (values: T) => Promise<unknown>;
}

export function createForm<T extends Record<string, unknown>>(options: CreateFormOptions<T>) {
	const values = $state<T>({ ...options.initial });
	const errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);
	let submitting = $state<boolean>(false);

	const clearErrors = () => {
		for (const key of Object.keys(errors)) {
			delete errors[key];
		}
		formError = null;
	};

	const focusFirstError = (fields: string[]) => {
		if (typeof document === 'undefined') return;
		for (const field of fields) {
			const el = document.querySelector<HTMLElement>(`[name="${field}"]`);
			if (el && typeof el.focus === 'function') {
				el.focus();
				break;
			}
		}
	};

	const submit = async (e?: SubmitEvent): Promise<boolean> => {
		if (e && typeof e.preventDefault === 'function') {
			e.preventDefault();
		}

		if (submitting) {
			return false;
		}

		clearErrors();

		// 1. Zod client validation
		const parsed = options.schema.safeParse(values);
		if (!parsed.success) {
			const errorFields: string[] = [];
			for (const issue of parsed.error.issues) {
				const field = issue.path[0];
				if (typeof field === 'string' && !errors[field]) {
					errors[field] = issue.message;
					errorFields.push(field);
				}
			}
			focusFirstError(errorFields);
			return false;
		}

		submitting = true;
		try {
			await options.onSubmit(parsed.data);
			return true;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'VALIDATION_ERROR') {
				const fields = err.fieldErrors;
				const errorFields: string[] = [];
				for (const [field, reason] of Object.entries(fields)) {
					const msg = fieldErrorMessage(field, reason);
					if (field in options.initial) {
						errors[field] = msg;
						errorFields.push(field);
					} else {
						// Field not recognized in form (e.g. body, cursor, query)
						formError = msg;
					}
				}

				if (errorFields.length > 0) {
					focusFirstError(errorFields);
				}
			} else {
				formError = errorMessage(err);
			}
			return false;
		} finally {
			submitting = false;
		}
	};

	const reset = (newValues?: Partial<T>) => {
		clearErrors();
		Object.assign(values, options.initial, newValues);
	};

	return {
		get values() {
			return values;
		},
		get errors() {
			return errors;
		},
		get formError() {
			return formError;
		},
		set formError(val: string | null) {
			formError = val;
		},
		get submitting() {
			return submitting;
		},
		set submitting(val: boolean) {
			submitting = val;
		},
		submit,
		reset,
		clearErrors
	};
}
