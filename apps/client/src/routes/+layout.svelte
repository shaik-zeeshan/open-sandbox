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
	const healthDebouncer = createDebouncer(400);

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
	<link rel="icon" href={favicon} />
	<link rel="preconnect" href="https://fonts.googleapis.com" />
	<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin="anonymous" />
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@300;400;500&family=Instrument+Serif:ital@0;1&display=swap" rel="stylesheet" />
</svelte:head>
{@render children()}
<Toaster />
