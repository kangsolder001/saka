import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import FakePaymentDialog from './FakePaymentDialog.svelte';
import type { Invoice } from '$lib/api/types';

const mockGoto = vi.fn().mockResolvedValue(undefined);
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => mockGoto(...args)
}));

const mockSimulateFakePayment = vi.fn().mockResolvedValue({ status: 'ok' });
vi.mock('$lib/dev/fake-payment', () => ({
	simulateFakePayment: (...args: unknown[]) => mockSimulateFakePayment(...args)
}));

const sampleInvoice: Invoice = {
	id: 'inv-uuid-123',
	number: 'INV-2026-000001',
	kind: 'checkout',
	plan: 'pro',
	plan_name: 'Pro',
	amount: 99000,
	currency: 'IDR',
	status: 'open',
	period_start: '2026-01-01T00:00:00Z',
	period_end: '2026-02-01T00:00:00Z',
	due_at: '2026-01-02T00:00:00Z',
	paid_at: null,
	payment_url: 'https://pay.fake.test/INV-2026-000001',
	created_at: '2026-01-01T00:00:00Z'
};

describe('FakePaymentDialog.svelte', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders invoice details in DEV mode', async () => {
		const screen = render(FakePaymentDialog, {
			open: true,
			invoice: sampleInvoice,
			orgId: 'org-1'
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('fake-payment-dialog')).toBeDefined();
			expect(screen.getByTestId('fake-invoice-number')).toBeDefined();
		});

		expect(screen.getByText('INV-2026-000001')).toBeDefined();
		expect(screen.getByText('Pro')).toBeDefined();
		expect(screen.getByTestId('fake-pay-btn')).toBeDefined();
		expect(screen.getByTestId('fake-cancel-btn')).toBeDefined();
	});

	it('calls simulateFakePayment and navigates to /billing/return on click Bayar', async () => {
		const onPaid = vi.fn();
		const screen = render(FakePaymentDialog, {
			open: true,
			invoice: sampleInvoice,
			orgId: 'org-1',
			onPaid
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('fake-pay-btn')).toBeDefined();
		});

		const payBtn = screen.getByTestId('fake-pay-btn');
		await payBtn.click();

		await vi.waitFor(() => {
			expect(mockSimulateFakePayment).toHaveBeenCalledWith({
				invoiceNumber: 'INV-2026-000001',
				amount: 99000,
				currency: 'IDR'
			});
			expect(onPaid).toHaveBeenCalled();
			expect(mockGoto).toHaveBeenCalledWith('/billing/return');
		});
	});

	it('calls onClose when clicking Batal', async () => {
		const onClose = vi.fn();
		const screen = render(FakePaymentDialog, {
			open: true,
			invoice: sampleInvoice,
			orgId: 'org-1',
			onClose
		});

		await vi.waitFor(() => {
			expect(screen.getByTestId('fake-cancel-btn')).toBeDefined();
		});

		const cancelBtn = screen.getByTestId('fake-cancel-btn');
		await cancelBtn.click();

		await vi.waitFor(() => {
			expect(onClose).toHaveBeenCalled();
		});
	});
});
