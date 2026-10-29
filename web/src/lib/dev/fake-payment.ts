export interface FakePaymentParams {
	invoiceNumber: string;
	amount: number;
	currency?: string;
	eventId?: string;
	providerRef?: string;
	secret?: string;
}

export async function signWebhook(secret: string, bodyText: string): Promise<string> {
	const enc = new TextEncoder();
	const key = await crypto.subtle.importKey(
		'raw',
		enc.encode(secret),
		{ name: 'HMAC', hash: 'SHA-256' },
		false,
		['sign']
	);
	const sig = await crypto.subtle.sign('HMAC', key, enc.encode(bodyText));
	return Array.from(new Uint8Array(sig))
		.map((b) => b.toString(16).padStart(2, '0'))
		.join('');
}

/**
 * DEV only: simulates customer payment by sending a signed webhook
 * to POST /api/v1/webhooks/fake (§8.6, docs/demo.md Step 5).
 */
export async function simulateFakePayment({
	invoiceNumber,
	amount,
	currency = 'IDR',
	eventId,
	providerRef,
	secret = 'fake-webhook-secret'
}: FakePaymentParams): Promise<{ status: string }> {
	const bodyObj = {
		event_id: eventId || `evt-${invoiceNumber}`,
		invoice_number: invoiceNumber,
		provider_ref: providerRef ?? 'dev-ref-' + Date.now(),
		state: 'paid',
		amount,
		currency
	};
	const body = JSON.stringify(bodyObj);
	const sig = await signWebhook(secret, body);

	const res = await fetch('/api/v1/webhooks/fake', {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json',
			'X-Fake-Signature': sig
		},
		body
	});

	if (!res.ok) {
		const text = await res.text();
		throw new Error(`Fake payment failed: ${res.status} ${text}`);
	}

	return res.json();
}
