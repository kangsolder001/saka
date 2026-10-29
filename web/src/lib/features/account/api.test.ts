import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	getMe,
	updateMe,
	changePassword,
	requestEmailChange,
	listSessions,
	revokeSession,
	deleteMe
} from './api';
import { client } from '$lib/api/client';

describe('account api', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('getMe calls GET /me', async () => {
		const mockResponse = {
			user: {
				id: 'u1',
				name: 'Test',
				email: 'test@example.com',
				avatar_url: null,
				email_verified: true
			},
			organizations: []
		};

		vi.spyOn(client, 'GET').mockResolvedValue({
			data: mockResponse,
			response: new Response(JSON.stringify(mockResponse), { status: 200 })
		} as never);

		const result = await getMe();
		expect(result).toEqual(mockResponse);
	});

	it('updateMe calls PATCH /me', async () => {
		const mockProfile = {
			id: 'u1',
			name: 'Updated Name',
			email: 'test@example.com',
			avatar_url: 'https://example.com/new.png',
			email_verified: true
		};

		const patchSpy = vi.spyOn(client, 'PATCH').mockResolvedValue({
			data: { user: mockProfile },
			response: new Response(JSON.stringify({ user: mockProfile }), { status: 200 })
		} as never);

		const result = await updateMe({ name: 'Updated Name' });
		expect(result.user).toEqual(mockProfile);
		expect(patchSpy).toHaveBeenCalledWith('/me', {
			body: { name: 'Updated Name' }
		});
	});

	it('changePassword calls POST /me/password', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { message: 'Password changed' },
			response: new Response(JSON.stringify({ message: 'Password changed' }), { status: 200 })
		} as never);

		const result = await changePassword('oldPass', 'newPass123');
		expect(result.message).toBe('Password changed');
		expect(postSpy).toHaveBeenCalledWith('/me/password', {
			body: { current_password: 'oldPass', new_password: 'newPass123' }
		});
	});

	it('requestEmailChange calls POST /me/email', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { message: 'Confirmation sent' },
			response: new Response(JSON.stringify({ message: 'Confirmation sent' }), { status: 200 })
		} as never);

		const result = await requestEmailChange('new@example.com', 'pass123');
		expect(result.message).toBe('Confirmation sent');
		expect(postSpy).toHaveBeenCalledWith('/me/email', {
			body: { new_email: 'new@example.com', current_password: 'pass123' }
		});
	});

	it('listSessions calls GET /me/sessions', async () => {
		const mockSessions = [
			{
				id: 's1',
				user_agent: 'Mozilla/5.0',
				ip: '127.0.0.1',
				created_at: '2026-01-01T00:00:00Z',
				last_used_at: '2026-01-01T00:00:00Z',
				expires_at: '2026-01-08T00:00:00Z',
				current: true
			}
		];

		vi.spyOn(client, 'GET').mockResolvedValue({
			data: { sessions: mockSessions },
			response: new Response(JSON.stringify({ sessions: mockSessions }), { status: 200 })
		} as never);

		const result = await listSessions();
		expect(result).toEqual(mockSessions);
	});

	it('revokeSession calls DELETE /me/sessions/{id}', async () => {
		const deleteSpy = vi.spyOn(client, 'DELETE').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await revokeSession('s1');
		expect(deleteSpy).toHaveBeenCalledWith('/me/sessions/{id}', {
			params: { path: { id: 's1' } }
		});
	});

	it('deleteMe calls DELETE /me', async () => {
		const deleteSpy = vi.spyOn(client, 'DELETE').mockResolvedValue({
			data: undefined,
			response: new Response(null, { status: 204 })
		} as never);

		await deleteMe('confirmPass');
		expect(deleteSpy).toHaveBeenCalledWith('/me', {
			body: { current_password: 'confirmPass' }
		});
	});
});
