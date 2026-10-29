export const queryKeys = {
	me: ['me'] as const,
	plans: ['plans'] as const,
	organization: (orgId: string) => ['orgs', orgId] as const,
	members: (orgId: string) => ['orgs', orgId, 'members'] as const,
	invitations: (orgId: string) => ['orgs', orgId, 'invitations'] as const,
	subscription: (orgId: string) => ['orgs', orgId, 'subscription'] as const,
	invoices: (orgId: string) => ['orgs', orgId, 'invoices'] as const,
	invoice: (orgId: string, invoiceId: string) => ['orgs', orgId, 'invoices', invoiceId] as const,
	sessions: ['me', 'sessions'] as const,
	invitationByToken: (token: string) => ['invitation', token] as const
};
