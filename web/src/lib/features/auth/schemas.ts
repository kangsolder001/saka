import { z } from 'zod';
import * as m from '$lib/paraglide/messages';

export const loginSchema = z.object({
	email: z.string().trim().min(1, m.field_error_required()).email(m.field_error_invalid()),
	password: z.string().min(1, m.field_error_required())
});

export type LoginInput = z.infer<typeof loginSchema>;

export const registerSchema = z
	.object({
		name: z.string().trim().min(1, m.field_error_required()).max(100),
		email: z.string().trim().min(1, m.field_error_required()).email(m.field_error_invalid()),
		password: z
			.string()
			.min(8, m.auth_reset_password_length_error())
			.max(72, m.auth_reset_password_length_error()),
		organization_name: z.string().trim().min(1, m.field_error_required()).max(100)
	})
	.refine((data) => data.password !== data.email, {
		message: m.auth_reset_password_same_email_error(),
		path: ['password']
	});

export type RegisterInput = z.infer<typeof registerSchema>;

export const forgotPasswordSchema = z.object({
	email: z.string().trim().min(1, m.field_error_required()).email(m.field_error_invalid())
});

export type ForgotPasswordInput = z.infer<typeof forgotPasswordSchema>;

export const resetPasswordSchema = z
	.object({
		password: z
			.string()
			.min(8, m.auth_reset_password_length_error())
			.max(72, m.auth_reset_password_length_error()),
		confirmPassword: z.string().min(1, m.field_error_required())
	})
	.refine((data) => data.password === data.confirmPassword, {
		message: m.auth_reset_password_match_error(),
		path: ['confirmPassword']
	});

export type ResetPasswordInput = z.infer<typeof resetPasswordSchema>;
