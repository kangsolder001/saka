import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import RoleBadge from './RoleBadge.svelte';

describe('RoleBadge', () => {
	it('renders owner role badge', async () => {
		const screen = render(RoleBadge, {
			role: 'owner'
		});
		expect(screen.getByText('Pemilik')).toBeDefined();
	});

	it('renders admin and member roles', async () => {
		const screenAdmin = render(RoleBadge, {
			role: 'admin'
		});
		expect(screenAdmin.getByText('Admin')).toBeDefined();

		const screenMember = render(RoleBadge, {
			role: 'member'
		});
		expect(screenMember.getByText('Anggota')).toBeDefined();
	});
});
