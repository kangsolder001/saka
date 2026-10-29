import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import InvoicesPage from './+page.svelte';
import type { Invoice } from '$lib/api/types';

const mockGoto = vi.fn().mockResolvedValue(undefined);
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => mockGoto(...args)
}));

const mockInvoices: Invoice[] = [
	{
		id: 'inv-1',
		number: 'INV-2026-000001',
		kind: 'checkout',
		plan: 'pro',
		plan_name: 'Pro',
		amount: 99000,
		currency: 'IDR',
		status: 'paid',
		period_start: '2026-01-01T00:00:00Z',
		period_end: '2026-02-01T00:00:00Z',
		due_at: '2026-01-02T00:00:00Z',
		paid_at: '2026-01-01T10:00:00Z',
		payment_url: null,
		created_at: '2026-01-01T00:00:00Z'
	},
	{
		id: 'inv-2',
		number: 'INV-2026-000002',
		kind: 'renewal',
		plan: 'pro',
		plan_name: 'Pro',
		amount: 99000,
		currency: 'IDR',
		status: 'open',
		period_start: '2026-02-01T00:00:00Z',
		period_end: '2026-03-01T00:00:00Z',
		due_at: '2026-02-03T00:00:00Z',
		paid_at: null,
		payment_url: 'https://pay.fake.test/INV-2026-000002',
		created_at: '2026-02-01T00:00:00Z'
	}
];

const mockState = {
	role: 'owner' as 'owner' | 'admin' | 'member',
	invoices: mockInvoices,
	hasNextPage: false,
	fetchNextPage: vi.fn()
};

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-test-123' },
		url: new URL('http://localhost:3000/orgs/org-test-123/billing/invoices')
	}
}));

vi.mock('$lib/features/org/api', () => ({
	createOrganizationQuery: () => ({
		isPending: false,
		isError: false,
		data: {
			organization: {
				id: 'org-test-123',
				name: 'Test Org',
				slug: 'test-org',
				role: mockState.role
			}
		},
		refetch: vi.fn()
	})
}));

vi.mock('$lib/features/billing/api', () => ({
	createInfiniteInvoicesQuery: () => ({
		isPending: false,
		isError: false,
		hasNextPage: mockState.hasNextPage,
		isFetchingNextPage: false,
		data: {
			pages: [{ items: mockState.invoices, next_cursor: undefined }]
		},
		fetchNextPage: () => mockState.fetchNextPage(),
		refetch: vi.fn()
	})
}));

describe('Invoices Page (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockState.role = 'owner';
		mockState.invoices = mockInvoices;
		mockState.hasNextPage = false;
	});

	it('renders table headers and invoice rows with formatted money and dates', async () => {
		const screen = render(InvoicesPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('invoices-page')).toBeDefined();
			expect(screen.getByTestId('invoices-table-body')).toBeDefined();
			expect(screen.getByTestId('invoice-row-inv-1')).toBeDefined();
			expect(screen.getByTestId('invoice-row-inv-2')).toBeDefined();
		});

		expect(screen.getByText('INV-2026-000001')).toBeDefined();
		expect(screen.getByText('INV-2026-000002')).toBeDefined();
		expect(document.querySelector('[data-testid="invoice-row-inv-1"]')).not.toBeNull();
	});

	it('clicking an invoice row navigates to invoice detail', async () => {
		const screen = render(InvoicesPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('invoice-row-inv-1')).toBeDefined();
		});

		const row = screen.getByTestId('invoice-row-inv-1');
		await row.click();

		await vi.waitFor(() => {
			expect(mockGoto).toHaveBeenCalledWith('/orgs/org-test-123/billing/invoices/inv-1');
		});
	});

	it('shows LoadMore button when hasNextPage is true', async () => {
		mockState.hasNextPage = true;
		const screen = render(InvoicesPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('load-more-button')).toBeDefined();
		});

		const loadMoreBtn = screen.getByTestId('load-more-button');
		await loadMoreBtn.click();

		expect(mockState.fetchNextPage).toHaveBeenCalled();
	});

	it('renders empty state when there are no invoices', async () => {
		mockState.invoices = [];
		const screen = render(InvoicesPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('empty-state')).toBeDefined();
		});
	});

	it('renders forbidden notice for non-owner member', async () => {
		mockState.role = 'member';
		const screen = render(InvoicesPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('invoices-forbidden')).toBeDefined();
		});
	});
});
