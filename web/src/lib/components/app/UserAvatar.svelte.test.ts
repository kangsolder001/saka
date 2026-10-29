import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import UserAvatar from './UserAvatar.svelte';

describe('UserAvatar', () => {
	it('renders initials when no avatar URL is provided', async () => {
		const screen = render(UserAvatar, {
			name: 'John Doe',
			email: 'john@example.com'
		});

		expect(screen.getByText('JD')).toBeDefined();
	});

	it('renders fallback for single word name or email', async () => {
		const screen = render(UserAvatar, {
			name: 'Alice'
		});

		expect(screen.getByText('AL')).toBeDefined();
	});

	it('renders fallback for null name and email', async () => {
		const screen = render(UserAvatar, {});
		expect(screen.getByText('?')).toBeDefined();
	});

	it('ignores invalid javascript: or data: avatar URLs', async () => {
		const screen = render(UserAvatar, {
			name: 'Bob Smith',
			avatarUrl: 'javascript:alert(1)'
		});

		// Should fall back to initials BS
		expect(screen.getByText('BS')).toBeDefined();
	});

	it('renders image when valid http(s) URL is provided', async () => {
		const screen = render(UserAvatar, {
			name: 'Charlie Brown',
			avatarUrl: 'https://example.com/avatar.jpg'
		});

		const img = screen.getByRole('img');
		expect(img).toBeDefined();
	});
});
