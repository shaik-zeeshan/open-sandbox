<script lang="ts">
	import { goto } from "$app/navigation";
	import { page } from "$app/state";
	import { onMount } from "svelte";
	import { checkHealth, handleAuthError, refreshAuthSession, restoreSession } from "$lib/auth-controller.svelte";
	import { clearScheduledTimeout, createDebouncer, onAuthErrorEvent, scheduleTimeout } from "$lib/client/browser";
	import { clientState } from "$lib/stores.svelte";
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import Toaster from '$lib/components/Toaster.svelte';

	let { children } = $props();
	const APP_NAME = "Open Sandbox";
	const healthDebouncer = createDebouncer(400);

	const formatSegment = (segment: string): string => {
		const decoded = decodeURIComponent(segment).replace(/[-_]+/g, " ").trim();
		if (decoded.length === 0) {
			return "";
		}
		return decoded.replace(/\b\w/g, (char) => char.toUpperCase());
	};

	const routeTitle = $derived.by(() => {
		const path = page.url.pathname;
		if (path === "/") {
			return "Dashboard";
		}
		if (path === "/settings") {
			return "Settings";
		}
		if (path === "/users") {
			return "User Access";
		}
		if (path === "/images") {
			return "Images";
		}
		if (path === "/compose") {
			return "Compose";
		}
		if (path.startsWith("/compose/")) {
			const project = formatSegment(path.slice("/compose/".length));
			return project.length > 0 ? `Compose: ${project}` : "Compose";
		}
		if (path.startsWith("/sandboxes/")) {
			const sandbox = formatSegment(path.slice("/sandboxes/".length));
			return sandbox.length > 0 ? `Sandbox: ${sandbox}` : "Sandbox";
		}
		if (path.startsWith("/services/")) {
			const service = formatSegment(path.slice("/services/".length));
			return service.length > 0 ? `Service: ${service}` : "Service";
		}

		const lastSegment = path.split("/").filter(Boolean).at(-1);
		return lastSegment ? formatSegment(lastSegment) : "Dashboard";
	});

	const pageTitle = $derived(`${routeTitle} - ${APP_NAME}`);

	$effect(() => {
		clientState.baseUrl;
		healthDebouncer.trigger(() => {
			void checkHealth();
		});
		return () => {
			healthDebouncer.cancel();
		};
	});

	$effect(() => {
		if (!clientState.isAuthenticated || clientState.tokenExpiresAt === null) {
			return;
		}

		const refreshAndHandleFailure = async (): Promise<void> => {
			if (await refreshAuthSession()) {
				return;
			}

			handleAuthError();
			if (page.url.pathname !== "/") {
				await goto("/");
			}
		};

		const delay = clientState.tokenExpiresAt * 1000 - Date.now() - 60_000;
		if (delay <= 0) {
			void refreshAndHandleFailure();
			return;
		}

		const timer = scheduleTimeout(() => {
			void refreshAndHandleFailure();
		}, delay);
		return () => clearScheduledTimeout(timer);
	});

	$effect(() => {
		if (page.url.pathname === "/") {
			return;
		}

		if (clientState.authResolved && !clientState.isAuthenticated) {
			void goto("/");
		}
	});

	onMount(() => {
		void restoreSession();
		const cleanup = onAuthErrorEvent(() => {
			handleAuthError();
			if (page.url.pathname !== "/") {
				void goto("/");
			}
		});
		return cleanup;
	});
</script>

<svelte:head>
	<title>{pageTitle}</title>
	<link rel="icon" href={favicon} />
	<link rel="preconnect" href="https://fonts.googleapis.com" />
	<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin="anonymous" />
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@300;400;500&family=Instrument+Serif:ital@0;1&display=swap" rel="stylesheet" />
</svelte:head>
{@render children()}
<Toaster />
