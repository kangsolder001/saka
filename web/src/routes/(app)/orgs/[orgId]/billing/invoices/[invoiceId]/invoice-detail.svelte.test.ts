import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import InvoiceDetailPage from './+page.svelte';
import type { Invoice } from '$lib/api/types';

const mockGoto = vi.fn().mockResolvedValue(undefined);
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => mockGoto(...args)
}));

const mockGoToPayment = vi.fn();
vi.mock('$lib/features/billing/purchase', () => ({
	goToPayment: (...args: unknown[]) => mockGoToPayment(...args)
}));

const sampleOpenInvoice: Invoice = {
	id: 'inv-test-123',
	number: 'INV-2026-000042',
	kind: 'checkout',
	plan: 'pro',
	plan_name: 'Pro',
	amount: 99000,
	currency: 'IDR',
	status: 'open',
	period_start: '2026-02-01T00:00:00Z',
	period_end: '2026-03-01T00:00:00Z',
	due_at: '2026-02-05T00:00:00Z',
	paid_at: null,
	payment_url: 'https://pay.fake.test/INV-2026-000042',
	created_at: '2026-02-01T00:00:00Z'
};

const mockState = {
	invoice: sampleOpenInvoice as Invoice | null
};

vi.mock('$app/state', () => ({
	page: {
		params: { orgId: 'org-123', invoiceId: 'inv-test-123' },
		url: new URL('http://localhost:3000/orgs/org-123/billing/invoices/inv-test-123')
	}
}));

vi.mock('$lib/features/billing/api', () => ({
	createInvoiceQuery: () => ({
		isPending: false,
		isError: false,
		data: mockState.invoice,
		refetch: vi.fn()
	})
}));

describe('Invoice Detail Page (+page.svelte)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockState.invoice = sampleOpenInvoice;
	});

	it('renders invoice metadata: number, amount, plan, period, due_at, status', async () => {
		const screen = render(InvoiceDetailPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('invoice-detail-page')).toBeDefined();
			expect(screen.getByTestId('invoice-number')).toBeDefined();
		});

		expect(screen.getByText('Invoice #INV-2026-000042')).toBeDefined();
		expect(screen.getByTestId('invoice-amount')).toBeDefined();
		expect(screen.getByText('Pro')).toBeDefined();
		expect(screen.getByTestId('invoice-period')).toBeDefined();
		expect(screen.getByTestId('invoice-due-at')).toBeDefined();
		expect(screen.getByTestId('invoice-paid-at')).toBeDefined();
	});

	it('renders Bayar sekarang button for open invoice and triggers goToPayment on click', async () => {
		const screen = render(InvoiceDetailPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('pay-invoice-btn')).toBeDefined();
		});

		const payBtn = screen.getByTestId('pay-invoice-btn');
		await payBtn.click();

		await vi.waitFor(() => {
			expect(mockGoToPayment).toHaveBeenCalledWith({
				orgId: 'org-123',
				invoice: sampleOpenInvoice
			});
		});
	});

	it('does not render Bayar sekarang button if invoice is paid', async () => {
		mockState.invoice = {
			...sampleOpenInvoice,
			status: 'paid',
			paid_at: '2026-02-02T10:00:00Z',
			payment_url: null
		};

		const screen = render(InvoiceDetailPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('invoice-detail-page')).toBeDefined();
		});

		expect(document.querySelector('[data-testid="pay-invoice-btn"]')).toBeNull();
	});

	it('back button navigates back to invoices list', async () => {
		const screen = render(InvoiceDetailPage);

		await vi.waitFor(() => {
			expect(screen.getByTestId('back-button')).toBeDefined();
		});

		const backBtn = screen.getByTestId('back-button');
		await backBtn.click();

		await vi.waitFor(() => {
			expect(mockGoto).toHaveBeenCalledWith('/orgs/org-123/billing/invoices');
		});
	});
});
