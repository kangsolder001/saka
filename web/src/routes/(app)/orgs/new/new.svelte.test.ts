import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import NewOrgPage from './+page.svelte';
import { createOrganization, getLastOrgId } from '$lib/features/org/api';
import { goto } from '$app/navigation';
import { toast } from 'svelte-sonner';
import { ApiError } from '$lib/api/errors';
import * as m from '$lib/paraglide/messages';

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			user: { id: 'u1', name: 'Test User', email: 'test@example.com' },
			organizations: []
		}
	})
}));

vi.mock('$lib/features/org/api', async (importOriginal) => {
	const actual = await importOriginal<typeof import('$lib/features/org/api')>();
	return {
		...actual,
		createOrganization: vi.fn()
	};
});

vi.mock('svelte-sonner', () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn()
	}
}));

describe('NewOrgPage (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	it('renders form fields properly', async () => {
		const screen = render(NewOrgPage);

		expect(screen.getByText(m.org_new_title())).toBeDefined();
		expect(screen.getByTestId('org-name-input')).toBeDefined();
		expect(screen.getByTestId('org-slug-input')).toBeDefined();
		expect(screen.getByTestId('submit-org-btn')).toBeDefined();
		expect(screen.getByTestId('cancel-btn')).toBeDefined();
	});

	it('validates required name field', async () => {
		const screen = render(NewOrgPage);

		const submitBtn = screen.getByTestId('submit-org-btn');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('org-name-error')).toBeDefined();
		});
		expect(createOrganization).not.toHaveBeenCalled();
	});

	it('validates invalid slug format', async () => {
		const screen = render(NewOrgPage);

		const nameInput = screen.getByTestId('org-name-input');
		const slugInput = screen.getByTestId('org-slug-input');
		const submitBtn = screen.getByTestId('submit-org-btn');

		await nameInput.fill('Acme Corp');
		await slugInput.fill('Invalid Slug!');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('org-slug-error')).toBeDefined();
		});
		expect(createOrganization).not.toHaveBeenCalled();
	});

	it('submits successfully and redirects to new org', async () => {
		vi.mocked(createOrganization).mockResolvedValue({
			organization: {
				id: 'org-abc-123',
				name: 'Acme Corp',
				slug: 'acme-corp',
				logo_url: null,
				owner_id: 'u1',
				created_at: new Date().toISOString(),
				updated_at: new Date().toISOString(),
				role: 'owner'
			}
		});

		const screen = render(NewOrgPage);

		const nameInput = screen.getByTestId('org-name-input');
		const slugInput = screen.getByTestId('org-slug-input');
		const submitBtn = screen.getByTestId('submit-org-btn');

		await nameInput.fill('Acme Corp');
		await slugInput.fill('acme-corp');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(createOrganization).toHaveBeenCalledWith({
				name: 'Acme Corp',
				slug: 'acme-corp'
			});
			expect(getLastOrgId()).toBe('org-abc-123');
			expect(toast.success).toHaveBeenCalledWith(m.org_create_success());
			expect(goto).toHaveBeenCalledWith('/orgs/org-abc-123');
		});
	});

	it('submits successfully without optional slug', async () => {
		vi.mocked(createOrganization).mockResolvedValue({
			organization: {
				id: 'org-auto-slug',
				name: 'Auto Slug Org',
				slug: 'auto-slug-org',
				logo_url: null,
				owner_id: 'u1',
				created_at: new Date().toISOString(),
				updated_at: new Date().toISOString(),
				role: 'owner'
			}
		});

		const screen = render(NewOrgPage);

		const nameInput = screen.getByTestId('org-name-input');
		const submitBtn = screen.getByTestId('submit-org-btn');

		await nameInput.fill('Auto Slug Org');
		await submitBtn.click();

		await vi.waitFor(() => {
			expect(createOrganization).toHaveBeenCalledWith({
				name: 'Auto Slug Org',
				slug: undefined
			});
			expect(getLastOrgId()).toBe('org-auto-slug');
			expect(goto).toHaveBeenCalledWith('/orgs/org-auto-slug');
		});
	});

	it('handles 409 conflict error on slug', async () => {
		vi.mocked(createOrganization).mockRejectedValue(
			new ApiError({
				status: 409,
				code: 'CONFLICT',
				serverMessage: 'Slug sudah dipakai'
			})
		);

		const screen = render(NewOrgPage);

		const nameInput = screen.getByTestId('org-name-input');
		const slugInput = screen.getByTestId('org-slug-input');
		const submitBtn = screen.getByTestId('submit-org-btn');

		await nameInput.fill('Existing Org');
		await slugInput.fill('existing-org');
		await submitBtn.click();

		await vi.waitFor(() => {
			const slugError = screen.getByTestId('org-slug-error');
			expect(slugError.element().textContent).toContain('Slug sudah dipakai');
		});
	});

	it('handles 429 rate limit error', async () => {
		vi.mocked(createOrganization).mockRejectedValue(
			new ApiError({
				status: 429,
				code: 'RATE_LIMITED',
				serverMessage: 'Too many requests',
				retryAfterSeconds: 30
			})
		);

		const screen = render(NewOrgPage);

		const nameInput = screen.getByTestId('org-name-input');
		const submitBtn = screen.getByTestId('submit-org-btn');

		await nameInput.fill('Rate Limited Org');
		await submitBtn.click();

		await vi.waitFor(() => {
			const formError = screen.getByTestId('form-error');
			expect(formError).toBeDefined();
		});
	});
});
