import type { Role } from '$lib/api/types';

export type OrgAction =
	| 'org.update'
	| 'org.delete'
	| 'org.leave'
	| 'ownership.transfer'
	| 'invitations.manage'
	| 'member.changeRole'
	| 'member.remove'
	| 'billing.manage'
	| 'invoices.view';

export interface TargetMember {
	userId?: string;
	role?: Role;
}

/**
 * Pure authorization check for UI components based on §9.1 permissions matrix.
 * No direct role checks (e.g. `role === 'owner'`) should be done in UI components.
 */
export function can(
	callerRole: Role | undefined | null,
	action: OrgAction,
	target?: TargetMember,
	selfUserId?: string
): boolean {
	if (!callerRole) return false;

	switch (action) {
		case 'org.update':
			return callerRole === 'owner' || callerRole === 'admin';

		case 'org.delete':
			return callerRole === 'owner';

		case 'org.leave':
			return callerRole === 'admin' || callerRole === 'member';

		case 'ownership.transfer':
			return callerRole === 'owner';

		case 'invitations.manage':
			return callerRole === 'owner' || callerRole === 'admin';

		case 'member.changeRole':
		case 'member.remove': {
			if (callerRole === 'member') return false;

			if (target) {
				if (selfUserId && target.userId && target.userId === selfUserId) {
					return false;
				}
				if (callerRole === 'owner') {
					return target.role !== 'owner';
				}
				if (callerRole === 'admin') {
					return target.role === 'member';
				}
			}
			return true;
		}

		case 'billing.manage':
			return callerRole === 'owner';

		case 'invoices.view':
			return callerRole === 'owner';

		default:
			return false;
	}
}
