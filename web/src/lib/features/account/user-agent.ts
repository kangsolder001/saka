export interface UserAgentInfo {
	browser: string;
	os: string;
	formatted: string;
}

export function describeUserAgent(userAgent: string): UserAgentInfo {
	if (!userAgent || typeof userAgent !== 'string') {
		return {
			browser: 'Unknown Browser',
			os: 'Unknown OS',
			formatted: 'Unknown Device'
		};
	}

	const ua = userAgent;

	// Detect OS
	let os = 'Unknown OS';
	if (/Android/i.test(ua)) {
		os = 'Android';
	} else if (/iPhone|iPad|iPod/i.test(ua)) {
		os = 'iOS';
	} else if (/Macintosh|Mac OS X/i.test(ua)) {
		os = 'macOS';
	} else if (/Windows|Win32|Win64/i.test(ua)) {
		os = 'Windows';
	} else if (/Linux/i.test(ua)) {
		os = 'Linux';
	}

	// Detect Browser (order matters: Edge before Chrome, Chrome before Safari)
	let browser = 'Unknown Browser';
	if (/Edg(?:e)?\//i.test(ua)) {
		browser = 'Edge';
	} else if (/Firefox|FxiOS/i.test(ua)) {
		browser = 'Firefox';
	} else if (/Chrome|CriOS/i.test(ua)) {
		browser = 'Chrome';
	} else if (/Safari/i.test(ua) && !/Chrome|CriOS|Edg/i.test(ua)) {
		browser = 'Safari';
	}

	let formatted: string;
	if (browser === 'Unknown Browser' && os === 'Unknown OS') {
		formatted = 'Unknown Device';
	} else if (browser === 'Unknown Browser') {
		formatted = os;
	} else if (os === 'Unknown OS') {
		formatted = browser;
	} else {
		formatted = `${browser} on ${os}`;
	}

	return {
		browser,
		os,
		formatted
	};
}
