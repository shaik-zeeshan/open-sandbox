import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, '', '');
	const userAllowedHosts = (env.VITE_ALLOWED_HOSTS ?? '')
		.split(',')
		.map((host) => host.trim())
		.filter(Boolean);

	const allowedHosts =
		userAllowedHosts.length === 0
			? true
			: Array.from(new Set(['localhost', '127.0.0.1', '::1', ...userAllowedHosts]));

	return {
		plugins: [tailwindcss(), sveltekit()],
		server: {
			allowedHosts
		}
	};
});
