import { call, client } from '$lib/api/client';
import type { InvitationInfo, OrganizationEnvelope } from '$lib/api/types';

export async function getInvitation(token: string): Promise<InvitationInfo> {
	const res = await call(() =>
		client.GET('/invitations/{token}', {
			params: {
				path: { token }
			}
		})
	);
	return res.invitation;
}

export async function acceptInvitation(token: string): Promise<OrganizationEnvelope> {
	return call(() =>
		client.POST('/invitations/{token}/accept', {
			params: {
				path: { token }
			}
		})
	);
}

export async function declineInvitation(token: string): Promise<void> {
	await call(() =>
		client.POST('/invitations/{token}/decline', {
			params: {
				path: { token }
			}
		})
	);
}
