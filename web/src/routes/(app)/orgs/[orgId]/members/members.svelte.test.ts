import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import MembersPage from './+page.svelte';

const { mockState } = vi.hoisted(() => {
	const mockMembers = [
		{
			user_id: 'user-owner-1',
			name: 'Owner User',
			email: 'owner@example.com',
			avatar_url: null,
			role: 'owner' as const,
			joined_at: '2026-01-01T00:00:00Z'
		},
		{
			user_id: 'user-member-2',
			name: 'Jane Member',
			email: 'jane@example.com',
			avatar_url: null,
			role: 'member' as const,
			joined_at: '2026-01-10T00:00:00Z'
		}
	];

	const mockInvitations = [
		{
			id: 'inv-1',
			email: 'pending@example.com',
			role: 'member' as const,
			invited_by: 'user-owner-1',
			expires_at: '2026-02-15T00:00:00Z',
			created_at: '2026-01-28T00:00:00Z'
		}
	];

	return {
		mockState: {
			role: 'owner' as 'owner' | 'admin' | 'member',
			members: mockMembers,
			invitations: mockInvitations
		}
	};
});

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:5173/orgs/org-test-123/members')
	}
}));

vi.mock('$lib/features/org/api', () => ({
	createOrganizationQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			organization: {
				id: 'org-test-123',
				name: 'Acme Test Corp',
				slug: 'acme-test',
				role: mockState.role
			}
		}
	})
}));

vi.mock('$lib/features/account/api', () => ({
	createMeQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			user: {
				id: 'user-owner-1',
				email: 'owner@example.com',
				name: 'Owner User'
			}
		}
	})
}));

vi.mock('$lib/features/members/api', () => ({
	createMembersQuery: () => ({
		isPending: false,
		isError: false,
		hasNextPage: false,
		isFetchingNextPage: false,
		data: {
			pages: [{ items: mockState.members, next_cursor: null }]
		},
		refetch: vi.fn()
	}),
	changeMemberRole: vi.fn(),
	removeMember: vi.fn()
}));

vi.mock('$lib/features/invitations/api', () => ({
	createInvitationsQuery: () => ({
		isPending: false,
		isError: false,
		hasNextPage: false,
		isFetchingNextPage: false,
		data: {
			pages: [{ items: mockState.invitations, next_cursor: null }]
		},
		refetch: vi.fn()
	}),
	createInvitation: vi.fn(),
	resendInvitation: vi.fn(),
	revokeInvitation: vi.fn()
}));

vi.mock('$lib/features/billing/api', () => ({
	createSubscriptionQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			plan: 'pro',
			effective_plan: 'pro',
			status: 'active',
			usage: {
				members: { used: 2, max: 10 }
			}
		},
		refetch: vi.fn()
	})
}));

vi.mock('@tanstack/svelte-query', async (importOriginal) => {
	const actual = await importOriginal<typeof import('@tanstack/svelte-query')>();
	return {
		...actual,
		useQueryClient: () => ({
			invalidateQueries: vi.fn().mockResolvedValue(undefined)
		})
	};
});

describe('Members Page (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockState.role = 'owner';
		mockState.invitations = [
			{
				id: 'inv-1',
				email: 'pending@example.com',
				role: 'member',
				invited_by: 'user-owner-1',
				expires_at: '2026-02-15T00:00:00Z',
				created_at: '2026-01-28T00:00:00Z'
			}
		];
	});

	it('renders members page with header, tabs, and member rows', async () => {
		const screen = render(MembersPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('members-page')).toBeDefined();
			expect(screen.getByTestId('tab-trigger-members')).toBeDefined();
			expect(screen.getByTestId('tab-trigger-invitations')).toBeDefined();
			expect(screen.getByTestId('invite-member-btn')).toBeDefined();
		});

		// Check member usage
		expect(screen.getByTestId('member-usage')).toBeDefined();

		// Check members rendered
		expect(screen.getByText('Owner User')).toBeDefined();
		expect(screen.getByText('Jane Member')).toBeDefined();
		expect(screen.getByTestId('member-row-user-owner-1')).toBeDefined();
		expect(screen.getByTestId('member-row-user-member-2')).toBeDefined();
	});

	it('switches to invitations tab and renders pending invitations', async () => {
		const screen = render(MembersPage);

		const invTab = screen.getByTestId('tab-trigger-invitations');
		await invTab.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('tab-content-invitations')).toBeDefined();
			expect(screen.getByTestId('invitation-row-inv-1')).toBeDefined();
			expect(screen.getByText('pending@example.com')).toBeDefined();
		});
	});

	it('renders empty state when there are no invitations', async () => {
		mockState.invitations = [];
		const screen = render(MembersPage);

		const invTab = screen.getByTestId('tab-trigger-invitations');
		await invTab.click();

		await vi.waitFor(() => {
			expect(screen.getByTestId('empty-state')).toBeDefined();
		});
	});

	it('does not show invite button for regular member', async () => {
		mockState.role = 'member';
		const screen = render(MembersPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('members-page')).toBeDefined();
		});

		expect(document.querySelector('[data-testid="invite-member-btn"]')).toBeNull();
	});
});
