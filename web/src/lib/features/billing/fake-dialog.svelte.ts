import type { Invoice } from '$lib/api/types';

export const fakePaymentDialog = $state<{
	open: boolean;
	orgId: string;
	invoice: Invoice | null;
}>({
	open: false,
	orgId: '',
	invoice: null
});

export function openFakePaymentDialog(params: { orgId: string; invoice: Invoice }): void {
	fakePaymentDialog.open = true;
	fakePaymentDialog.orgId = params.orgId;
	fakePaymentDialog.invoice = params.invoice;
}

export function closeFakePaymentDialog(): void {
	fakePaymentDialog.open = false;
	fakePaymentDialog.orgId = '';
	fakePaymentDialog.invoice = null;
}
