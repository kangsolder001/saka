import { describe, expect, it, vi, beforeEach } from 'vitest';
import { signWebhook, simulateFakePayment } from './fake-payment';

describe('fake-payment (dev webhook simulation)', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('computes expected HMAC-SHA256 hex signature', async () => {
		const body = JSON.stringify({
			event_id: 'evt-1',
			invoice_number: 'INV-2026-000001',
			provider_ref: 'ref-1',
			state: 'paid',
			amount: 99000,
			currency: 'IDR'
		});
		const sig = await signWebhook('fake-webhook-secret', body);
		expect(sig).toBe('357a9f420e18ec9b8d25578bd135e12ea9391685dd4404616c8bdd0ff6bf0ff3');
	});

	it('sends signed webhook to /api/v1/webhooks/fake', async () => {
		const mockFetch = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({ status: 'ok' })
		});
		vi.stubGlobal('fetch', mockFetch);

		const res = await simulateFakePayment({
			invoiceNumber: 'INV-2026-000001',
			amount: 99000,
			currency: 'IDR',
			eventId: 'evt-1',
			providerRef: 'ref-1'
		});

		expect(res).toEqual({ status: 'ok' });
		expect(mockFetch).toHaveBeenCalledTimes(1);

		const firstCall = mockFetch.mock.calls[0];
		expect(firstCall).toBeDefined();
		const [url, init] = firstCall!;
		expect(url).toBe('/api/v1/webhooks/fake');
		expect(init.method).toBe('POST');
		expect(init.headers['Content-Type']).toBe('application/json');
		expect(init.headers['X-Fake-Signature']).toBe(
			'357a9f420e18ec9b8d25578bd135e12ea9391685dd4404616c8bdd0ff6bf0ff3'
		);
		const parsedBody = JSON.parse(init.body);
		expect(parsedBody.invoice_number).toBe('INV-2026-000001');
		expect(parsedBody.state).toBe('paid');
		expect(parsedBody.amount).toBe(99000);
	});

	it('throws error when webhook returns non-200', async () => {
		const mockFetch = vi.fn().mockResolvedValue({
			ok: false,
			status: 400,
			text: async () => 'Invalid signature'
		});
		vi.stubGlobal('fetch', mockFetch);

		await expect(
			simulateFakePayment({
				invoiceNumber: 'INV-bad',
				amount: 1000
			})
		).rejects.toThrow('Fake payment failed: 400 Invalid signature');
	});
});
