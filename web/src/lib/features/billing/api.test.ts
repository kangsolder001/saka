import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	getPlans,
	createPlansQuery,
	getSubscription,
	createSubscriptionQuery,
	listInvoices,
	createInvoicesQuery,
	getInvoice,
	createInvoiceQuery,
	checkout,
	changePlan,
	cancelSubscription,
	resumeSubscription
} from './api';
import { client } from '$lib/api/client';
import { createQueryClient } from '$lib/api/query-client';
import { queryKeys } from '$lib/api/query-keys';
import type {
	Plan,
	Subscription,
	Invoice,
	CheckoutResponse,
	ChangePlanResponse
} from '$lib/api/types';

describe('billing api', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('getPlans calls GET /plans and unwraps items', async () => {
		const mockPlans: Plan[] = [
			{
				code: 'free',
				name: 'Free',
				price_amount: 0,
				currency: 'IDR',
				interval: 'month',
				trial_days: 0,
				features: [],
				limits: { members: 3 }
			},
			{
				code: 'pro',
				name: 'Pro',
				price_amount: 99000,
				currency: 'IDR',
				interval: 'month',
				trial_days: 14,
				features: ['export_csv'],
				limits: { members: 10 }
			}
		];

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: { items: mockPlans },
			response: new Response(JSON.stringify({ items: mockPlans }), { status: 200 })
		} as never);

		const result = await getPlans();

		expect(getSpy).toHaveBeenCalledWith('/plans');
		expect(result).toEqual(mockPlans);
	});

	it('createPlansQuery configures TanStack query', () => {
		const qc = createQueryClient();
		const query = createPlansQuery(() => qc);
		expect(query).toBeDefined();
		expect(queryKeys.plans).toEqual(['plans']);
	});

	it('getSubscription calls GET /orgs/{orgId}/subscription and unwraps subscription', async () => {
		const mockSub: Subscription = {
			plan: 'pro',
			effective_plan: 'pro',
			status: 'active',
			current_period_start: '2026-10-01T00:00:00Z',
			current_period_end: '2026-11-01T00:00:00Z',
			cancel_at_period_end: false,
			pending_plan: null,
			trial_ends_at: null,
			grace_until: null,
			usage: {
				members: { used: 2, max: 10 }
			}
		};

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: { subscription: mockSub },
			response: new Response(JSON.stringify({ subscription: mockSub }), { status: 200 })
		} as never);

		const result = await getSubscription('org-1');

		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/subscription', {
			params: { path: { orgId: 'org-1' } }
		});
		expect(result).toEqual(mockSub);
	});

	it('createSubscriptionQuery configures TanStack query with function or string orgId', () => {
		const qc = createQueryClient();
		const queryFn = createSubscriptionQuery(
			() => 'org-123',
			() => qc
		);
		expect(queryFn).toBeDefined();

		const queryString = createSubscriptionQuery('org-123', () => qc);
		expect(queryString).toBeDefined();

		expect(queryKeys.subscription('org-123')).toEqual(['orgs', 'org-123', 'subscription']);
	});

	it('listInvoices calls GET /orgs/{orgId}/invoices with object options or positional arguments', async () => {
		const mockInvoices: Invoice[] = [
			{
				id: 'inv-1',
				number: 'INV-2026-0001',
				kind: 'checkout',
				plan: 'pro',
				plan_name: 'Pro',
				amount: 99000,
				currency: 'IDR',
				status: 'paid',
				period_start: '2026-10-01T00:00:00Z',
				period_end: '2026-11-01T00:00:00Z',
				due_at: '2026-10-08T00:00:00Z',
				paid_at: '2026-10-01T05:00:00Z',
				payment_url: null,
				created_at: '2026-10-01T00:00:00Z'
			}
		];
		const mockResponse = {
			items: mockInvoices,
			next_cursor: 'cursor-next'
		};

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: mockResponse,
			response: new Response(JSON.stringify(mockResponse), { status: 200 })
		} as never);

		// With object params
		const res1 = await listInvoices('org-1', { limit: 10, cursor: 'cur-1' });
		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/invoices', {
			params: {
				path: { orgId: 'org-1' },
				query: { cursor: 'cur-1', limit: 10 }
			}
		});
		expect(res1).toEqual(mockResponse);

		// With positional arguments
		const res2 = await listInvoices('org-1', 'cur-2', 5);
		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/invoices', {
			params: {
				path: { orgId: 'org-1' },
				query: { cursor: 'cur-2', limit: 5 }
			}
		});
		expect(res2).toEqual(mockResponse);

		// Without arguments
		await listInvoices('org-1');
		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/invoices', {
			params: {
				path: { orgId: 'org-1' },
				query: {}
			}
		});
	});

	it('createInvoicesQuery configures TanStack infinite query', () => {
		const qc = createQueryClient();
		const query = createInvoicesQuery(
			() => 'org-123',
			() => qc
		);
		expect(query).toBeDefined();
		expect(queryKeys.invoices('org-123')).toEqual(['orgs', 'org-123', 'invoices']);
	});

	it('getInvoice calls GET /orgs/{orgId}/invoices/{id} and unwraps invoice', async () => {
		const mockInvoice: Invoice = {
			id: 'inv-42',
			number: 'INV-2026-0042',
			kind: 'checkout',
			plan: 'pro',
			plan_name: 'Pro',
			amount: 99000,
			currency: 'IDR',
			status: 'open',
			period_start: '2026-10-01T00:00:00Z',
			period_end: '2026-11-01T00:00:00Z',
			due_at: '2026-10-08T00:00:00Z',
			paid_at: null,
			payment_url: 'https://pay.fake.test/INV-2026-0042',
			created_at: '2026-10-01T00:00:00Z'
		};

		const getSpy = vi.spyOn(client, 'GET').mockResolvedValue({
			data: { invoice: mockInvoice },
			response: new Response(JSON.stringify({ invoice: mockInvoice }), { status: 200 })
		} as never);

		const result = await getInvoice('org-1', 'inv-42');

		expect(getSpy).toHaveBeenCalledWith('/orgs/{orgId}/invoices/{id}', {
			params: { path: { orgId: 'org-1', id: 'inv-42' } }
		});
		expect(result).toEqual(mockInvoice);
	});

	it('createInvoiceQuery configures TanStack query', () => {
		const qc = createQueryClient();
		const query = createInvoiceQuery(
			() => 'org-1',
			() => 'inv-1',
			() => qc
		);
		expect(query).toBeDefined();
		expect(queryKeys.invoice('org-1', 'inv-1')).toEqual(['orgs', 'org-1', 'invoices', 'inv-1']);
	});

	it('checkout calls POST /orgs/{orgId}/subscription/checkout and invalidates subscription & invoices', async () => {
		const qc = createQueryClient();
		const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

		const mockResponse = {
			trial_started: true,
			invoice: {
				id: 'inv-1',
				number: 'INV-2026-0001',
				kind: 'checkout',
				plan: 'pro',
				plan_name: 'Pro',
				amount: 99000,
				currency: 'IDR',
				status: 'open',
				period_start: '2026-10-01T00:00:00Z',
				period_end: '2026-11-01T00:00:00Z',
				due_at: '2026-10-08T00:00:00Z',
				paid_at: null,
				payment_url: 'https://pay.fake.test/INV-2026-0001',
				created_at: '2026-10-01T00:00:00Z'
			}
		} as unknown as CheckoutResponse;

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: mockResponse,
			response: new Response(JSON.stringify(mockResponse), { status: 201 })
		} as never);

		const result = await checkout('org-99', { plan: 'pro' }, qc);

		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/subscription/checkout', {
			params: { path: { orgId: 'org-99' } },
			body: { plan: 'pro' }
		});
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.subscription('org-99') });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.invoices('org-99') });
		expect(result).toEqual(mockResponse);
	});

	it('changePlan calls POST /orgs/{orgId}/subscription/change-plan and invalidates subscription & invoices', async () => {
		const qc = createQueryClient();
		const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

		const mockResponse = {
			scheduled: true,
			warnings: []
		} as unknown as ChangePlanResponse;

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: mockResponse,
			response: new Response(JSON.stringify(mockResponse), { status: 200 })
		} as never);

		const result = await changePlan('org-99', { plan: 'free' }, qc);

		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/subscription/change-plan', {
			params: { path: { orgId: 'org-99' } },
			body: { plan: 'free' }
		});
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.subscription('org-99') });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.invoices('org-99') });
		expect(result).toEqual(mockResponse);
	});

	it('cancelSubscription calls POST /orgs/{orgId}/subscription/cancel and invalidates subscription & invoices', async () => {
		const qc = createQueryClient();
		const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

		const mockSub: Subscription = {
			plan: 'pro',
			effective_plan: 'pro',
			status: 'active',
			current_period_start: '2026-10-01T00:00:00Z',
			current_period_end: '2026-11-01T00:00:00Z',
			cancel_at_period_end: true,
			pending_plan: null,
			trial_ends_at: null,
			grace_until: null,
			usage: {
				members: { used: 2, max: 10 }
			}
		};

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { subscription: mockSub },
			response: new Response(JSON.stringify({ subscription: mockSub }), { status: 200 })
		} as never);

		const result = await cancelSubscription('org-99', qc);

		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/subscription/cancel', {
			params: { path: { orgId: 'org-99' } }
		});
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.subscription('org-99') });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.invoices('org-99') });
		expect(result).toEqual(mockSub);
	});

	it('resumeSubscription calls POST /orgs/{orgId}/subscription/resume and invalidates subscription & invoices', async () => {
		const qc = createQueryClient();
		const invalidateSpy = vi.spyOn(qc, 'invalidateQueries');

		const mockSub: Subscription = {
			plan: 'pro',
			effective_plan: 'pro',
			status: 'active',
			current_period_start: '2026-10-01T00:00:00Z',
			current_period_end: '2026-11-01T00:00:00Z',
			cancel_at_period_end: false,
			pending_plan: null,
			trial_ends_at: null,
			grace_until: null,
			usage: {
				members: { used: 2, max: 10 }
			}
		};

		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { subscription: mockSub },
			response: new Response(JSON.stringify({ subscription: mockSub }), { status: 200 })
		} as never);

		const result = await resumeSubscription('org-99', qc);

		expect(postSpy).toHaveBeenCalledWith('/orgs/{orgId}/subscription/resume', {
			params: { path: { orgId: 'org-99' } }
		});
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.subscription('org-99') });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.invoices('org-99') });
		expect(result).toEqual(mockSub);
	});
});
