import { beforeEach, describe, expect, it, vi } from 'vitest';
import { handlePurchaseResult, goToPayment } from './purchase';
import { fakePaymentDialog } from './fake-dialog.svelte';
import { scheduledDowngradeDialog } from './downgrade-dialog.svelte';
import { toast } from 'svelte-sonner';
import type { QueryClient } from '@tanstack/svelte-query';
import type { CheckoutResponse, ChangePlanResponse, Invoice, Subscription } from '$lib/api/types';

const mockGoto = vi.fn().mockResolvedValue(undefined);
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => mockGoto(...args)
}));

vi.mock('svelte-sonner', () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
		info: vi.fn()
	}
}));

const mockQC = {
	invalidateQueries: vi.fn().mockResolvedValue(undefined)
} as unknown as QueryClient;

class MockStorage implements Storage {
	private store = new Map<string, string>();
	get length() {
		return this.store.size;
	}
	clear() {
		this.store.clear();
	}
	getItem(key: string) {
		return this.store.get(key) ?? null;
	}
	key(index: number) {
		return Array.from(this.store.keys())[index] ?? null;
	}
	removeItem(key: string) {
		this.store.delete(key);
	}
	setItem(key: string, value: string) {
		this.store.set(key, String(value));
	}
}

const mockSessionStorage = new MockStorage();
vi.stubGlobal('sessionStorage', mockSessionStorage);

const sampleSub: Subscription = {
	plan: 'pro',
	status: 'active',
	trial_ends_at: '2026-02-15T00:00:00Z',
	current_period_start: '2026-02-01T00:00:00Z',
	current_period_end: '2026-03-01T00:00:00Z',
	cancel_at_period_end: false,
	pending_plan: null,
	grace_until: null,
	effective_plan: 'pro',
	usage: {}
};

const sampleInvoice: Invoice = {
	id: 'inv-123',
	number: 'INV-2026-000001',
	kind: 'checkout',
	plan: 'pro',
	plan_name: 'Pro',
	amount: 99000,
	currency: 'IDR',
	status: 'open',
	period_start: '2026-02-01T00:00:00Z',
	period_end: '2026-03-01T00:00:00Z',
	due_at: '2026-02-02T00:00:00Z',
	paid_at: null,
	payment_url: 'https://pay.fake.test/INV-2026-000001',
	created_at: '2026-02-01T00:00:00Z'
};

describe('purchase.ts alur (§8.3)', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		sessionStorage.clear();
		fakePaymentDialog.open = false;
		fakePaymentDialog.invoice = null;
		scheduledDowngradeDialog.open = false;
		scheduledDowngradeDialog.warnings = [];
	});

	it('1. trial_started === true -> shows toast, invalidates subscription & invoices, navigates to /orgs/{orgId}/billing', async () => {
		const res: CheckoutResponse = {
			trial_started: true,
			subscription: sampleSub,
			invoice: sampleInvoice
		};

		await handlePurchaseResult(res, 'org-abc', mockQC);

		expect(toast.success).toHaveBeenCalledWith(expect.stringContaining('pro'));
		expect(mockQC.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ['orgs', 'org-abc', 'subscription']
		});
		expect(mockQC.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ['orgs', 'org-abc', 'invoices']
		});
		expect(mockGoto).toHaveBeenCalledWith('/orgs/org-abc/billing');
	});

	it('2. scheduled === true -> opens scheduled downgrade dialog with warnings', async () => {
		const res: ChangePlanResponse = {
			trial_started: false,
			scheduled: true,
			cleared: false,
			subscription: {
				...sampleSub,
				pending_plan: 'free'
			},
			invoice: null,
			warnings: [{ limit: 'members', used: 5, max: 3 }]
		};

		const onScheduledDowngrade = vi.fn();
		await handlePurchaseResult(res, 'org-abc', mockQC, { onScheduledDowngrade });

		expect(onScheduledDowngrade).toHaveBeenCalledWith({
			pendingPlan: 'free',
			currentPeriodEnd: '2026-03-01T00:00:00Z',
			warnings: [{ limit: 'members', used: 5, max: 3 }]
		});
		expect(scheduledDowngradeDialog.open).toBe(true);
		expect(scheduledDowngradeDialog.warnings).toEqual([{ limit: 'members', used: 5, max: 3 }]);
		expect(mockQC.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ['orgs', 'org-abc', 'subscription']
		});
	});

	it('3. cleared === true -> toast "Perubahan plan dibatalkan", refetches subscription', async () => {
		const res: ChangePlanResponse = {
			trial_started: false,
			scheduled: false,
			cleared: true,
			subscription: sampleSub,
			invoice: null,
			warnings: []
		};

		await handlePurchaseResult(res, 'org-abc', mockQC);

		expect(toast.success).toHaveBeenCalledWith('Perubahan plan dibatalkan');
		expect(mockQC.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ['orgs', 'org-abc', 'subscription']
		});
	});

	it('4. invoice.payment_url with pay.fake.test in DEV -> saves to sessionStorage and opens FakePaymentDialog', async () => {
		const res: CheckoutResponse = {
			trial_started: false,
			subscription: sampleSub,
			invoice: sampleInvoice
		};

		const onFakePayment = vi.fn();
		await handlePurchaseResult(res, 'org-abc', mockQC, { onFakePayment });

		const stored = sessionStorage.getItem('saka.pendingPayment');
		expect(stored).toBe(JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' }));
		expect(onFakePayment).toHaveBeenCalledWith(sampleInvoice);
		expect(fakePaymentDialog.open).toBe(true);
		expect(fakePaymentDialog.invoice).toEqual(sampleInvoice);
	});

	it('4b. invoice.payment_url with real external URL -> saves to sessionStorage and redirects', () => {
		const mockAssign = vi.fn();
		vi.stubGlobal('window', {
			location: {
				assign: mockAssign
			}
		});

		const realInvoice: Invoice = {
			...sampleInvoice,
			payment_url: 'https://app.midtrans.com/snap/v2/vtweb/test-123'
		};

		goToPayment({ orgId: 'org-abc', invoice: realInvoice });

		const stored = sessionStorage.getItem('saka.pendingPayment');
		expect(stored).toBe(JSON.stringify({ orgId: 'org-abc', invoiceId: 'inv-123' }));
		expect(mockAssign).toHaveBeenCalledWith('https://app.midtrans.com/snap/v2/vtweb/test-123');

		vi.unstubAllGlobals();
		vi.stubGlobal('sessionStorage', mockSessionStorage);
	});

	it('5. else -> refetches subscription', async () => {
		const res: CheckoutResponse = {
			trial_started: false,
			subscription: sampleSub,
			invoice: { ...sampleInvoice, payment_url: null }
		};

		await handlePurchaseResult(res, 'org-abc', mockQC);

		expect(mockQC.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ['orgs', 'org-abc', 'subscription']
		});
	});
});
