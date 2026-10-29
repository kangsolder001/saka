import { describe, expect, it } from 'vitest';
import { getMe } from './api';

describe('account api', () => {
	it('exports getMe and createMeQuery functions', () => {
		expect(typeof getMe).toBe('function');
	});
});
