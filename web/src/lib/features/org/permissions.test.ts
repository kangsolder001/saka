import { describe, it, expect } from 'vitest';
import { can } from './permissions';

describe('can() permission matrix (§9.1)', () => {
	it('returns false when callerRole is null or undefined', () => {
		expect(can(undefined, 'org.update')).toBe(false);
		expect(can(null, 'org.update')).toBe(false);
	});

	describe('org.update', () => {
		it('allows owner and admin, denies member', () => {
			expect(can('owner', 'org.update')).toBe(true);
			expect(can('admin', 'org.update')).toBe(true);
			expect(can('member', 'org.update')).toBe(false);
		});
	});

	describe('org.delete', () => {
		it('allows only owner', () => {
			expect(can('owner', 'org.delete')).toBe(true);
			expect(can('admin', 'org.delete')).toBe(false);
			expect(can('member', 'org.delete')).toBe(false);
		});
	});

	describe('org.leave', () => {
		it('allows admin and member, denies owner', () => {
			expect(can('owner', 'org.leave')).toBe(false);
			expect(can('admin', 'org.leave')).toBe(true);
			expect(can('member', 'org.leave')).toBe(true);
		});
	});

	describe('ownership.transfer', () => {
		it('allows only owner', () => {
			expect(can('owner', 'ownership.transfer')).toBe(true);
			expect(can('admin', 'ownership.transfer')).toBe(false);
			expect(can('member', 'ownership.transfer')).toBe(false);
		});
	});

	describe('invitations.manage', () => {
		it('allows owner and admin, denies member', () => {
			expect(can('owner', 'invitations.manage')).toBe(true);
			expect(can('admin', 'invitations.manage')).toBe(true);
			expect(can('member', 'invitations.manage')).toBe(false);
		});
	});

	describe('billing.manage', () => {
		it('allows only owner', () => {
			expect(can('owner', 'billing.manage')).toBe(true);
			expect(can('admin', 'billing.manage')).toBe(false);
			expect(can('member', 'billing.manage')).toBe(false);
		});
	});

	describe('invoices.view', () => {
		it('allows only owner', () => {
			expect(can('owner', 'invoices.view')).toBe(true);
			expect(can('admin', 'invoices.view')).toBe(false);
			expect(can('member', 'invoices.view')).toBe(false);
		});
	});

	describe('member.changeRole & member.remove', () => {
		const selfId = 'user-self';
		const otherMember = { userId: 'user-m', role: 'member' as const };
		const otherAdmin = { userId: 'user-a', role: 'admin' as const };
		const otherOwner = { userId: 'user-o', role: 'owner' as const };
		const selfTargetAsAdmin = { userId: selfId, role: 'admin' as const };
		const selfTargetAsMember = { userId: selfId, role: 'member' as const };

		it('member caller cannot change role or remove anyone', () => {
			expect(can('member', 'member.changeRole', otherMember, selfId)).toBe(false);
			expect(can('member', 'member.remove', otherMember, selfId)).toBe(false);
			expect(can('member', 'member.changeRole')).toBe(false);
			expect(can('member', 'member.remove')).toBe(false);
		});

		it('owner caller can change/remove other members and admins, but not self or another owner', () => {
			expect(can('owner', 'member.changeRole', otherMember, selfId)).toBe(true);
			expect(can('owner', 'member.remove', otherMember, selfId)).toBe(true);
			expect(can('owner', 'member.changeRole', otherAdmin, selfId)).toBe(true);
			expect(can('owner', 'member.remove', otherAdmin, selfId)).toBe(true);

			// Owner cannot target self
			expect(can('owner', 'member.changeRole', { userId: selfId, role: 'owner' }, selfId)).toBe(
				false
			);
			expect(can('owner', 'member.remove', { userId: selfId, role: 'owner' }, selfId)).toBe(false);

			// Owner cannot target another owner
			expect(can('owner', 'member.changeRole', otherOwner, selfId)).toBe(false);
			expect(can('owner', 'member.remove', otherOwner, selfId)).toBe(false);

			// General capability without target
			expect(can('owner', 'member.changeRole')).toBe(true);
			expect(can('owner', 'member.remove')).toBe(true);
		});

		it('admin caller can only change/remove member targets, not admins, owners, or self', () => {
			// Admin can target regular member
			expect(can('admin', 'member.changeRole', otherMember, selfId)).toBe(true);
			expect(can('admin', 'member.remove', otherMember, selfId)).toBe(true);

			// Admin cannot target other admin
			expect(can('admin', 'member.changeRole', otherAdmin, selfId)).toBe(false);
			expect(can('admin', 'member.remove', otherAdmin, selfId)).toBe(false);

			// Admin cannot target owner
			expect(can('admin', 'member.changeRole', otherOwner, selfId)).toBe(false);
			expect(can('admin', 'member.remove', otherOwner, selfId)).toBe(false);

			// Admin cannot target self
			expect(can('admin', 'member.changeRole', selfTargetAsAdmin, selfId)).toBe(false);
			expect(can('admin', 'member.remove', selfTargetAsAdmin, selfId)).toBe(false);
			expect(can('admin', 'member.changeRole', selfTargetAsMember, selfId)).toBe(false);
			expect(can('admin', 'member.remove', selfTargetAsMember, selfId)).toBe(false);

			// General capability without target
			expect(can('admin', 'member.changeRole')).toBe(true);
			expect(can('admin', 'member.remove')).toBe(true);
		});
	});
});
