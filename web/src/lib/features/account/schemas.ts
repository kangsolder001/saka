import { z } from 'zod';

export const profileSchema = z.object({
	name: z.string().trim().min(1, 'Name is required').max(100, 'Name is too long'),
	avatar_url: z
		.string()
		.trim()
		.max(2048, 'URL is too long')
		.refine(
			(val) => {
				if (!val) return true;
				return val.startsWith('http://') || val.startsWith('https://');
			},
			{ message: 'Avatar URL must start with http:// or https://' }
		)
		.nullable()
		.optional()
});

export type ProfileInput = z.infer<typeof profileSchema>;

export const changePasswordSchema = z
	.object({
		current_password: z
			.string()
			.min(1, 'Current password is required')
			.max(1024, 'Password is too long'),
		new_password: z
			.string()
			.min(8, 'Password must be at least 8 characters')
			.max(72, 'Password cannot exceed 72 characters'),
		confirm_password: z.string().min(1, 'Please confirm your password')
	})
	.refine((data) => data.new_password === data.confirm_password, {
		message: 'Passwords do not match',
		path: ['confirm_password']
	});

export type ChangePasswordInput = z.infer<typeof changePasswordSchema>;

export const changeEmailSchema = z.object({
	new_email: z.string().trim().email('Invalid email address').max(254, 'Email is too long'),
	current_password: z
		.string()
		.min(1, 'Current password is required')
		.max(1024, 'Password is too long')
});

export type ChangeEmailInput = z.infer<typeof changeEmailSchema>;

export const deleteAccountSchema = z.object({
	current_password: z
		.string()
		.min(1, 'Current password is required')
		.max(1024, 'Password is too long')
});

export type DeleteAccountInput = z.infer<typeof deleteAccountSchema>;
