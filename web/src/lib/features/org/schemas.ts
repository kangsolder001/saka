import { z } from 'zod';

export const SLUG_REGEX = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export const createOrgSchema = z.object({
	name: z.string().trim().min(1, 'Name is required').max(200, 'Maximum 200 characters'),
	slug: z
		.string()
		.trim()
		.optional()
		.refine((val) => !val || (val.length >= 3 && val.length <= 48 && SLUG_REGEX.test(val)), {
			message: 'Slug must be 3-48 lowercase letters, numbers, or hyphens'
		})
});

export type CreateOrgInput = z.infer<typeof createOrgSchema>;

export const updateOrgSchema = z
	.object({
		name: z
			.string()
			.trim()
			.min(1, 'Name is required')
			.max(200, 'Maximum 200 characters')
			.optional(),
		slug: z
			.string()
			.trim()
			.optional()
			.refine((val) => !val || (val.length >= 3 && val.length <= 48 && SLUG_REGEX.test(val)), {
				message: 'Slug must be 3-48 lowercase letters, numbers, or hyphens'
			}),
		logo_url: z
			.string()
			.trim()
			.optional()
			.refine(
				(val) => {
					if (!val) return true;
					try {
						const url = new URL(val);
						return url.protocol === 'http:' || url.protocol === 'https:';
					} catch {
						return false;
					}
				},
				{ message: 'Logo URL must start with http:// or https://' }
			)
	})
	.refine(
		(data) => data.name !== undefined || data.slug !== undefined || data.logo_url !== undefined,
		{
			message: 'At least one field must be provided'
		}
	);

export type UpdateOrgInput = z.infer<typeof updateOrgSchema>;

export const transferOwnershipSchema = z.object({
	user_id: z
		.string()
		.regex(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, 'Invalid user ID'),
	current_password: z.string().min(1, 'Password is required').max(1024, 'Password is too long')
});

export type TransferOwnershipInput = z.infer<typeof transferOwnershipSchema>;
