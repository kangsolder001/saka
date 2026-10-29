import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ReturnPage from './+page.svelte';
import * as navigation from '$app/navigation';
import * as guards from '$lib/auth/guards';
import * as billingApi from '$lib/features/billing/api';
import type { Invoice } from '$lib/api/types';
import { setLocale } from '$lib/i18n';

vi.mock('$app/navigation', () => ({
	goto: vi.fn().mockResolvedValue(undefined)
}));

vi.mock('$lib/auth/guards', () => ({
	ensureSession: vi.fn().mockResolvedValue(true)
}));

vi.mock('$lib/features/billing/api', async (importOriginal) => {
	const actual = await importOriginal<typeof import('$lib/features/billing/api')>();
	return {
		...actual,
		getInvoice: vi.fn()
	};
});

describe('Billing Return Page (+page.svelte)', () => {
	const PENDING_PAYMENT_KEY = 'saka.pendingPayment';

	const mockInvoice: Invoice = {
		id: 'inv-123',
		number: 'INV-2026-0001',
		plan: 'pro',
		plan_name: 'Pro',
		kind: 'checkout',
		amount: 99000,
		currency: 'IDR',
		status: 'paid',
		period_start: '2026-02-01T00:00:00Z',
		period_end: '2026-03-01T00:00:00Z',
		due_at: '2026-02-02T00:00:00Z',
		paid_at: '2026-02-01T00:00:00Z',
		created_at: '2026-02-01T00:00:00Z',
		payment_url: null
	};

	beforeEach(() => {
		vi.clearAllMocks();
		setLocale('id');
		sessionStorage.clear();
		document.body.innerHTML = '';
	});

	it('redirects to / if pendingPayment key is missing from sessionStorage', async () => {
		render(ReturnPage);

		await vi.waitFor(() => {
			expect(navigation.goto).toHaveBeenCalledWith('/');
		});
	});

	it('redirects to / if pendingPayment JSON is invalid or missing orgId/invoiceId', async () => {
		sessionStorage.setItem(PENDING_PAYMENT_KEY, 'invalid-json');
		render(ReturnPage);

		await vi.waitFor(() => {
			expect(navigation.goto).toHaveBeenCalledWith('/');
		});

		document.body.innerHTML = '';
		sessionStorage.setItem(PENDING_PAYMENT_KEY, JSON.stringify({ orgId: 'org-1' })); // missing invoiceId
		render(ReturnPage);

		await vi.waitFor(() => {
			expect(navigation.goto).toHaveBeenCalledWith('/');
		});
	});

	it('redirects to /login if ensureSession returns false', async () => {
		sessionStorage.setItem(
			PENDING_PAYMENT_KEY,
			JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' })
		);
		vi.mocked(guards.ensureSession).mockResolvedValue(false);

		render(ReturnPage);

		await vi.waitFor(() => {
			expect(navigation.goto).toHaveBeenCalledWith('/login');
		});
	});

	it('shows success card when invoice is paid, clears storage and provides link to billing', async () => {
		sessionStorage.setItem(
			PENDING_PAYMENT_KEY,
			JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' })
		);
		vi.mocked(guards.ensureSession).mockResolvedValue(true);
		vi.mocked(billingApi.getInvoice).mockResolvedValue({
			...mockInvoice,
			status: 'paid'
		});

		const screen = render(ReturnPage);

		await expect.element(screen.getByTestId('billing-return-paid')).toBeInTheDocument();
		expect(sessionStorage.getItem(PENDING_PAYMENT_KEY)).toBeNull();
		const billingBtn = screen.getByTestId('billing-return-billing-btn');
		expect(billingBtn.element().getAttribute('href')).toBe('/orgs/org-abc/billing');
	});

	it('shows invalid card when invoice is expired or void, provides link to plans', async () => {
		sessionStorage.setItem(
			PENDING_PAYMENT_KEY,
			JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' })
		);
		vi.mocked(guards.ensureSession).mockResolvedValue(true);
		vi.mocked(billingApi.getInvoice).mockResolvedValue({
			...mockInvoice,
			status: 'expired'
		});

		const screen = render(ReturnPage);

		await expect.element(screen.getByTestId('billing-return-invalid')).toBeInTheDocument();
		await expect.element(screen.getByText('Invoice tidak berlaku lagi')).toBeInTheDocument();
		const plansBtn = screen.getByTestId('billing-return-plans-btn');
		expect(plansBtn.element().getAttribute('href')).toBe('/orgs/org-abc/billing/plans');
	});

	it('shows timeout card when 60s elapses without final status, does not show gagal', async () => {
		sessionStorage.setItem(
			PENDING_PAYMENT_KEY,
			JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' })
		);
		vi.mocked(guards.ensureSession).mockResolvedValue(true);
		vi.mocked(billingApi.getInvoice).mockResolvedValue({
			...mockInvoice,
			status: 'open'
		});

		const screen = render(ReturnPage, { timeout: 0 });

		await expect.element(screen.getByTestId('billing-return-timeout')).toBeInTheDocument();
		await expect
			.element(screen.getByRole('heading', { name: 'Pembayaran Sedang Diproses' }))
			.toBeInTheDocument();
		expect(document.body.textContent).not.toContain('gagal');
		const billingBtn = screen.getByTestId('billing-return-billing-btn');
		expect(billingBtn.element().getAttribute('href')).toBe('/orgs/org-abc/billing');
	});
});
