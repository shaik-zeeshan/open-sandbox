<script lang="ts">
	import SandboxCard from "./SandboxCard.svelte";
	import Checkbox from "./Checkbox.svelte";
	import Combobox from "./Combobox.svelte";
	import PortsEditor from "./PortsEditor.svelte";
	import ProxyConfigEditor from "./ProxyConfigEditor.svelte";
	import EnvEditor from "./EnvEditor.svelte";
	import { resolveApiUrl, type ApiConfig, type ComposeProjectPreview, type ContainerSummary, type ImageSummary, type PreviewUrl, type Sandbox, type SandboxPortProxyConfig } from "$lib/api";
	import type { SandboxProgressDisplay } from "$lib/sandbox-progress";

	let {
		sandboxes,
		containers,
		composeProjects,
		images,
		config,
		loading,
		onOpen,
		onDuplicateSandbox,
		onRestart,
		onReset,
		onResetContainer,
		onStop,
		onDelete,
		onOpenContainer,
		onDuplicateContainer,
		onRestartContainer,
		onStopContainer,
		onRemoveContainer,
		onRefresh,
		composeHref = "/compose",
		showCreateForm,
		createDrawerInitialTab = "general",
		createDrawerInitialTabVersion = 0,
		createName = $bindable(),
		createExistingImage = $bindable(),
		createRepoUrl = $bindable(),
		createBranch = $bindable(),
		createBaseCommit = $bindable(),
		createDepth = $bindable(),
		createFilter = $bindable(),
		createSingleBranch = $bindable(),
		createWorkdir = $bindable(),
		createEnv = $bindable(),
		createSecretEnv = $bindable(),
		createSecretEnvHint = false,
		createPorts = $bindable(),
		createProxyConfig = $bindable(),
		createLoading,
		createProgress = null,
		createImageHref = "/images",
		onToggleCreate,
		onCreateSubmit,
		onApplyPreset
	} = $props<{
		sandboxes: Sandbox[];
		containers: ContainerSummary[];
		composeProjects: ComposeProjectPreview[];
		images: ImageSummary[];
		config: ApiConfig;
		loading: boolean;
		onOpen: (id: string) => void;
		onDuplicateSandbox: (id: string) => void;
		onRestart: (id: string) => void;
		onReset: (id: string) => void;
		onResetContainer: (id: string) => void;
		onStop: (id: string) => void;
		onDelete: (id: string) => void;
		onOpenContainer: (id: string) => void;
		onDuplicateContainer: (id: string) => void;
		onRestartContainer: (id: string) => void;
		onStopContainer: (id: string) => void;
		onRemoveContainer: (id: string) => void;
		onRefresh: () => void;
		composeHref?: string;
		showCreateForm: boolean;
		createDrawerInitialTab?: "general" | "git";
		createDrawerInitialTabVersion?: number;
		createName: string;
		createExistingImage: string;
		createRepoUrl: string;
		createBranch: string;
		createBaseCommit: string;
		createDepth: string;
		createFilter: string;
		createSingleBranch: boolean;
		createWorkdir: string;
		createEnv: string[];
		createSecretEnv: string[];
		createSecretEnvHint?: boolean;
		createPorts: string;
		createProxyConfig: Record<string, SandboxPortProxyConfig>;
		createLoading: boolean;
		createProgress?: SandboxProgressDisplay | null;
		createImageHref?: string;
		onToggleCreate: () => void;
		onCreateSubmit: () => void;
		onApplyPreset: (name: string, image: string) => void;
	}>();

	const presets = [
		{ label: "Ubuntu", name: "ubuntu-workspace", image: "ubuntu:24.04" },
		{ label: "Node",   name: "node-workspace",   image: "node:22"       },
		{ label: "Python", name: "python-workspace",  image: "python:3.12"  },
		{ label: "Go",     name: "go-workspace",      image: "golang:1.24"  }
	] as const;

	// Build combobox options from local images
	const localImageOptions = $derived(
		Array.from(
			new Set(images.flatMap((img: ImageSummary) => img.repo_tags.filter((t: string) => t !== "<none>:<none>"))) as Set<string>
		).map((tag: string) => ({ value: tag, label: tag }))
	);
	let workloadSearch = $state("");
	let workloadKindFilter = $state<"all" | "sandbox" | "container" | "compose">("all");
	let workloadStatusFilter = $state("all");

	// Parse distinct container port numbers from createPorts string for per-port proxy config
	const parsedProxyPorts = $derived.by(() => {
		const lines = createPorts.split("\n").map((l: string) => l.trim()).filter(Boolean);
		const ports: string[] = [];
		for (const line of lines) {
			const parts = line.split(":");
			const containerPort = (parts[1] ?? parts[0] ?? "").trim();
			if (containerPort.length > 0 && !ports.includes(containerPort)) {
				ports.push(containerPort);
			}
		}
		return ports;
	});

	// Inline validation state for the create drawer
	let nameError = $state("");
	let imageError = $state("");
	let repoUrlError = $state("");
	let depthError = $state("");
	let formTouched = $state(false);

	function isValidUrl(value: string): boolean {
		try {
			new URL(value);
			return true;
		} catch {
			return false;
		}
	}

	function isValidCloneDepth(value: string): boolean {
		const trimmed = value.trim();
		if (trimmed.length === 0) {
			return true;
		}

		if (!/^\d+$/.test(trimmed)) {
			return false;
		}

		const parsed = Number.parseInt(trimmed, 10);
		return Number.isInteger(parsed) && parsed > 0;
	}

	function validateForm(): boolean {
		let valid = true;
		let generalHasError = false;
		let gitHasError = false;
		nameError = "";
		imageError = "";
		repoUrlError = "";
		depthError = "";
		if (createName.trim().length === 0) {
			nameError = "Sandbox name is required.";
			valid = false;
			generalHasError = true;
		}
		if (createExistingImage.trim().length === 0) {
			imageError = "Select an image.";
			valid = false;
			generalHasError = true;
		}
		if (createRepoUrl.trim().length > 0 && !isValidUrl(createRepoUrl.trim())) {
			repoUrlError = "Enter a valid URL.";
			valid = false;
			gitHasError = true;
		}
		if (!isValidCloneDepth(createDepth)) {
			depthError = "Enter a positive integer or leave blank.";
			valid = false;
			gitHasError = true;
		}
		if (generalHasError) {
			activeDrawerTab = "general";
		} else if (gitHasError) {
			activeDrawerTab = "git";
		}
		return valid;
	}

	function handleSubmit(): void {
		formTouched = true;
		if (validateForm()) {
			onCreateSubmit();
		}
	}

	// Drawer tab state
	let activeDrawerTab = $state<"general" | "git" | "proxy">("general");

	// Reset validation state when drawer closes
	$effect(() => {
		if (!showCreateForm) {
			nameError = "";
			imageError = "";
			repoUrlError = "";
			depthError = "";
			formTouched = false;
			activeDrawerTab = "general";
		}
	});

	let lastAppliedCreateDrawerInitialTabVersion = $state(0);

	$effect(() => {
		if (!showCreateForm || createDrawerInitialTabVersion === lastAppliedCreateDrawerInitialTabVersion) {
			return;
		}

		activeDrawerTab = createDrawerInitialTab;
		lastAppliedCreateDrawerInitialTabVersion = createDrawerInitialTabVersion;
	});

	// Clear image error when a value is selected
	$effect(() => {
		if (createExistingImage.trim().length > 0) {
			imageError = "";
		}
	});

	$effect(() => {
		if (!formTouched) {
			return;
		}
		if (createName.trim().length > 0) {
			nameError = "";
		}
		if (createRepoUrl.trim().length === 0 || isValidUrl(createRepoUrl.trim())) {
			repoUrlError = "";
		}
		if (isValidCloneDepth(createDepth)) {
			depthError = "";
		}
	});
	const sandboxContainerIDs = $derived(new Set(sandboxes.map((sandbox: Sandbox) => sandbox.id)));
	const runtimeContainers = $derived(containers.filter((container: ContainerSummary) => !sandboxContainerIDs.has(container.id)));

	const composeLabelsForContainer = (container: ContainerSummary): { project: string; service: string } => {
		const project = (container.project_name ?? container.labels?.["com.docker.compose.project"] ?? "").trim();
		const service = (container.service_name ?? container.labels?.["com.docker.compose.service"] ?? "").trim();
		return { project, service };
	};

	const toTitleCase = (value: string): string =>
		value
			.split(/[-_\s]+/)
			.filter(Boolean)
			.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
			.join(" ");

	const normalizedStatus = (value: string): { value: string; label: string } => {
		const normalized = value.trim().toLowerCase();
		if (normalized.includes("up") || normalized.includes("running")) {
			return { value: "running", label: "Running" };
		}
		if (normalized.includes("exit") || normalized.includes("dead") || normalized.includes("error")) {
			return { value: "stopped", label: "Stopped" };
		}
		if (normalized.length === 0) {
			return { value: "unknown", label: "Unknown" };
		}
		return { value: normalized, label: toTitleCase(value.trim()) };
	};

	const workloadStatusOptions = [
		{ value: "running", label: "Running" },
		{ value: "stopped", label: "Stopped" },
		{ value: "idle", label: "Idle" },
		{ value: "unknown", label: "Unknown" }
	] as const;

	const normalizePreviewUrls = (entries: PreviewUrl[] | undefined): PreviewUrl[] =>
		(entries ?? [])
			.filter((entry) => entry.private_port > 0 && entry.url.trim().length > 0)
			.map((entry) => ({
				...entry,
				url: resolveApiUrl(config, entry.url)
			}));

		type WorkloadCardItem = {
			key: string;
			kind: "sandbox" | "container" | "compose";
			id: string;
			composeProjectName?: string;
		name: string;
		image: string;
		status: string;
		containerId: string;
		previewUrls: PreviewUrl[];
			createdAt: number | null;
			metaLabel: string;
			metaValue: string;
			canReset: boolean;
			showActions: boolean;
			statusFilterValue: string;
			statusFilterLabel: string;
			searchText: string;
		};

	const workloads = $derived.by<WorkloadCardItem[]>(() => {
		const sandboxItems = sandboxes.map((sandbox: Sandbox) => {
			const backingContainer = containers.find((c: ContainerSummary) => c.id === sandbox.id);
			const statusFilter = normalizedStatus(sandbox.status);
			const owner = sandbox.owner_username?.trim() ?? "";
			const workspaceDir = sandbox.workspace_dir.trim().length > 0 ? sandbox.workspace_dir : "/";
			return {
			key: `sandbox:${sandbox.id}`,
			kind: "sandbox" as const,
			id: sandbox.id,
			name: sandbox.name,
			image: sandbox.image,
			status: sandbox.status,
			containerId: sandbox.container_id,
			previewUrls: normalizePreviewUrls(backingContainer?.preview_urls ?? sandbox.preview_urls ?? []),
			createdAt: sandbox.created_at,
			metaLabel: "Sandbox",
			metaValue: owner.length > 0 ? `${owner} · ${workspaceDir}` : `Workspace ${workspaceDir}`,
			canReset: true,
			statusFilterValue: statusFilter.value,
			statusFilterLabel: statusFilter.label,
			searchText: [sandbox.name, sandbox.image, owner, workspaceDir, sandbox.id, sandbox.container_id].join(" ").toLowerCase()
			};
		});

		const containerItems = runtimeContainers.map((container: ContainerSummary) => {
			const { project: composeProject, service: composeService } = composeLabelsForContainer(container);
			const workloadKind = composeProject ? "compose" : "container";
			const primaryName = container.names[0] ?? container.id.slice(0, 12);
			const composeServiceName = composeService || primaryName;
			const metaValue = composeProject
				? `${composeProject} / ${composeServiceName}`
				: "Advanced / API-created";
			const statusFilter = normalizedStatus(container.state || container.status);
			return {
				key: `${workloadKind}:${container.id}`,
				kind: workloadKind,
				id: container.id,
				composeProjectName: composeProject || undefined,
				name: primaryName,
				image: container.image,
				status: container.status,
				containerId: container.container_id,
				previewUrls: normalizePreviewUrls(container.preview_urls ?? []),
				createdAt: container.created ?? null,
				metaLabel: composeProject ? "Compose service" : "Standalone container",
				metaValue,
				canReset: container.resettable,
				showActions: true,
				statusFilterValue: statusFilter.value,
				statusFilterLabel: statusFilter.label,
				searchText: [primaryName, container.image, metaValue, container.id, container.container_id, ...(container.names ?? [])].join(" ").toLowerCase()
			};
		});

		const composeProjectsInContainers = new Set(
			containerItems
				.filter((item: WorkloadCardItem) => item.kind === "compose" && (item.composeProjectName ?? "").length > 0)
				.map((item: WorkloadCardItem) => item.composeProjectName as string)
		);

		const composeProjectItems = composeProjectsWithPorts
			.filter((project) => !composeProjectsInContainers.has(project.projectName))
			.map((project) => {
				const previewUrls = project.ports.map((port) => ({
					private_port: port.privatePort,
					url: port.previewUrl
				}));
				const statusFilter = normalizedStatus(project.ports.length > 0 ? "running" : "idle");
				return {
					key: `compose-project:${project.projectName}`,
					kind: "compose" as const,
					id: `compose-project:${project.projectName}`,
					composeProjectName: project.projectName,
					name: project.projectName,
					image: "docker compose",
					status: project.ports.length > 0 ? "running" : "idle",
					containerId: "compose",
					previewUrls,
					createdAt: null,
					metaLabel: "Compose service",
					metaValue: `${project.projectName} / ${project.serviceCount} services`,
					canReset: false,
					showActions: false,
					statusFilterValue: statusFilter.value,
					statusFilterLabel: statusFilter.label,
					searchText: [project.projectName, "compose", `${project.serviceCount} services`].join(" ").toLowerCase()
				};
			});

		return [...sandboxItems, ...containerItems, ...composeProjectItems].sort((a, b) => {
			const createdDiff = (b.createdAt ?? 0) - (a.createdAt ?? 0);
			if (createdDiff !== 0) {
				return createdDiff;
			}
			return a.name.localeCompare(b.name);
		});
	});

	const filteredWorkloads = $derived.by(() => {
		const query = workloadSearch.trim().toLowerCase();
		return workloads.filter((item) => {
			if (workloadKindFilter !== "all" && item.kind !== workloadKindFilter) {
				return false;
			}
			if (workloadStatusFilter !== "all" && item.statusFilterValue !== workloadStatusFilter) {
				return false;
			}
			if (query.length > 0 && !item.searchText.includes(query)) {
				return false;
			}
			return true;
		});
	});

	const workloadCount = $derived(workloads.length);

	type ComposeProjectCard = {
		projectName: string;
		serviceCount: number;
		ports: Array<{ serviceName: string; privatePort: number; previewUrl: string }>;
	};

	const composeProjectsWithPorts = $derived.by<ComposeProjectCard[]>(() => {
		const projects = new Map<string, ComposeProjectCard>();

		for (const project of composeProjects) {
			const projectName = project.project_name.trim();
			if (projectName.length === 0) {
				continue;
			}
			projects.set(projectName, {
				projectName,
				serviceCount: project.services.length,
				ports: project.services.flatMap((service: ComposeProjectPreview["services"][number]) =>
					service.ports
						.filter((port: ComposeProjectPreview["services"][number]["ports"][number]) => port.private_port > 0 && port.preview_url.trim().length > 0)
						.map((port: ComposeProjectPreview["services"][number]["ports"][number]) => ({
							serviceName: service.service_name,
							privatePort: port.private_port,
							previewUrl: resolveApiUrl(config, port.preview_url)
						}))
				)
			});
		}

		for (const container of runtimeContainers) {
			const projectName = (container.project_name ?? container.labels?.["com.docker.compose.project"] ?? "").trim();
			if (projectName.length === 0) {
				continue;
			}
			const serviceName = (container.service_name ?? container.labels?.["com.docker.compose.service"] ?? "service").trim() || "service";
			const existing: ComposeProjectCard = projects.get(projectName) ?? { projectName, serviceCount: 0, ports: [] };

			const serviceNames = new Set(existing.ports.map((port) => port.serviceName));
			serviceNames.add(serviceName);
			existing.serviceCount = Math.max(existing.serviceCount, serviceNames.size);

			for (const preview of container.preview_urls ?? []) {
				if (preview.private_port <= 0 || preview.url.trim().length === 0) {
					continue;
				}
				const absolutePreviewUrl = resolveApiUrl(config, preview.url);
				if (existing.ports.some((item) => item.serviceName === serviceName && item.privatePort === preview.private_port && item.previewUrl === absolutePreviewUrl)) {
					continue;
				}
				existing.ports.push({
					serviceName,
					privatePort: preview.private_port,
					previewUrl: absolutePreviewUrl
				});
			}

			projects.set(projectName, existing);
		}

		return Array.from(projects.values()).sort((a, b) => a.projectName.localeCompare(b.projectName));
	});

	// Pagination
	const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;
	let pageSize = $state<10 | 25 | 50>(10);
	let currentPage = $state(1);

	// Reset to page 1 whenever filters/search change
	$effect(() => {
		// touch reactive deps
		workloadSearch; workloadKindFilter; workloadStatusFilter; pageSize;
		currentPage = 1;
	});

	const totalPages = $derived(Math.max(1, Math.ceil(filteredWorkloads.length / pageSize)));
	const pagedWorkloads = $derived(filteredWorkloads.slice((currentPage - 1) * pageSize, currentPage * pageSize));

	const goTo = (page: number) => {
		currentPage = Math.max(1, Math.min(page, totalPages));
	};

	const openComposePage = (projectName?: string): void => {
		const target = projectName && projectName.length > 0
			? `${composeHref}/${encodeURIComponent(projectName)}`
			: composeHref;
		if (typeof window !== "undefined") {
			window.location.assign(target);
		}
	};

	// Build visible page numbers: always show first, last, current ±1, with ellipsis
	const pageNumbers = $derived.by(() => {
		if (totalPages <= 7) return Array.from({ length: totalPages }, (_, i) => i + 1);
		const pages: (number | '…')[] = [];
		const add = (n: number) => { if (!pages.includes(n)) pages.push(n); };
		add(1);
		if (currentPage > 3) pages.push('…');
		for (let p = Math.max(2, currentPage - 1); p <= Math.min(totalPages - 1, currentPage + 1); p++) add(p);
		if (currentPage < totalPages - 2) pages.push('…');
		add(totalPages);
		return pages;
	});
</script>

<div class="list-view anim-fade-up">
	<!-- Page header -->
	<div class="list-header">
		<div class="list-title-group">
			<h1 class="list-title">Workloads</h1>
			{#if workloadCount > 0}
				<span class="list-count">{filteredWorkloads.length === workloadCount ? workloadCount : `${filteredWorkloads.length}/${workloadCount}`}</span>
			{/if}
		</div>
		<div class="list-actions">
			<label class="search-field" aria-label="Search workloads">
				<svg class="search-icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
				<input class="search-input" bind:value={workloadSearch} placeholder="Search workloads..." />
				{#if workloadSearch.length > 0}
					<button class="search-clear" type="button" onclick={() => workloadSearch = ""} title="Clear search" aria-label="Clear search">
						<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
							<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
						</svg>
					</button>
				{/if}
			</label>
			<div class="filter-group" aria-label="Filter workloads">
				<label class="filter-field">
					<span class="filter-label">Type</span>
					<select class="filter-select" bind:value={workloadKindFilter}>
						<option value="all">All</option>
						<option value="sandbox">Sandboxes</option>
						<option value="compose">Compose services</option>
						<option value="container">Standalone containers</option>
					</select>
				</label>
				<span class="filter-sep" aria-hidden="true"></span>
				<label class="filter-field">
					<span class="filter-label">Status</span>
					<select class="filter-select" bind:value={workloadStatusFilter}>
						<option value="all">All</option>
						{#each workloadStatusOptions as option (option.value)}
							<option value={option.value}>{option.label}</option>
						{/each}
					</select>
				</label>
			</div>
			<button class="btn-ghost btn-sm" type="button" onclick={onRefresh} disabled={loading}>
				{#if loading}
					<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{:else}
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
				{/if}
				Refresh
			</button>
			<a class="btn-ghost btn-sm" href={composeHref}>Compose</a>
			<button class="btn-primary btn-sm" type="button" onclick={onToggleCreate}>
				{showCreateForm ? "Cancel" : "+ New sandbox"}
			</button>
		</div>
	</div>

	<!-- Workload table list -->
	{#if loading && workloadCount === 0 && !showCreateForm}
		<div class="sandbox-table-wrap">
			<table class="sandbox-table">
				<thead>
					<tr class="thead-row">
						<th class="th">Name</th>
						<th class="th">Image</th>
						<th class="th">Status</th>
						<th class="th">Ports</th>
						<th class="th">Created</th>
						<th class="th">Container ID</th>
						<th class="th th-actions"></th>
					</tr>
				</thead>
				<tbody>
					{#each { length: 4 } as _, i}
						<tr class="skeleton-row">
							<td class="td-skel">
								<div class="skel-line skel-line--name"></div>
								<div class="skel-line skel-line--sub"></div>
							</td>
							<td class="td-skel"><div class="skel-line skel-line--image"></div></td>
							<td class="td-skel">
								<div class="skel-status">
									<div class="skel-dot"></div>
									<div class="skel-line skel-line--status"></div>
								</div>
							</td>
							<td class="td-skel"><div class="skel-line skel-line--port"></div></td>
							<td class="td-skel"><div class="skel-line skel-line--date"></div></td>
							<td class="td-skel"><div class="skel-line skel-line--id"></div></td>
							<td class="td-skel td-skel--actions">
								<div class="skel-actions">
									<div class="skel-btn"></div>
									<div class="skel-btn skel-btn--wide"></div>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if workloadCount === 0 && !showCreateForm}
		<div class="empty-state anim-fade-up anim-delay-1">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<rect x="2" y="7" width="20" height="14" rx="2"/><path d="M16 3H8l-2 4h12l-2-4z"/>
					<path d="M12 12v5M9.5 14.5l2.5-2.5 2.5 2.5"/>
				</svg>
			</div>
			<p class="empty-title">No workloads yet</p>
			<p class="empty-sub">Create a sandbox to get started with your development environment.</p>
			<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate}>+ New sandbox</button>
		</div>
	{:else if filteredWorkloads.length === 0}
		<div class="empty-state anim-fade-up anim-delay-1">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
			</div>
			<p class="empty-title">No workloads match</p>
			<p class="empty-sub">Try a different name, image, or workload id.</p>
		</div>
	{:else}
		<div class="sandbox-table-wrap">
			<table class="sandbox-table">
				<thead>
					<tr class="thead-row">
						<th class="th">Name</th>
						<th class="th">Image</th>
						<th class="th">Status</th>
						<th class="th">Ports</th>
						<th class="th">Created</th>
						<th class="th">Container ID</th>
						<th class="th th-actions"></th>
					</tr>
				</thead>
				<tbody>
					{#each pagedWorkloads as workload, i (workload.key)}
						<SandboxCard
							name={workload.name}
							image={workload.image}
							status={workload.status}
							containerId={workload.containerId}
							previewUrls={workload.previewUrls}
							createdAt={workload.createdAt}
							metaLabel={workload.metaLabel}
							metaValue={workload.metaValue}
							isSelected={false}
							showReset={workload.canReset}
							showActions={workload.showActions}
							showDuplicate={workload.showActions}
							deleteLabel={workload.kind === "sandbox" ? "Delete" : "Remove"}
							deleteTitle={workload.kind === "sandbox" ? "Delete sandbox" : "Remove container"}
							animDelay={i * 0.035}
							onOpen={() => workload.kind === "sandbox"
								? onOpen(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onOpenContainer(workload.id)}
							onDuplicate={() => workload.kind === "sandbox"
								? onDuplicateSandbox(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onDuplicateContainer(workload.id)}
							onRestart={() => workload.kind === "sandbox"
								? onRestart(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onRestartContainer(workload.id)}
							onReset={() => workload.kind === "sandbox"
								? onReset(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onResetContainer(workload.id)}
							onStop={() => workload.kind === "sandbox"
								? onStop(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onStopContainer(workload.id)}
							onDelete={() => workload.kind === "sandbox"
								? onDelete(workload.id)
								: workload.kind === "compose" && workload.id.startsWith("compose-project:")
									? openComposePage(workload.composeProjectName)
									: onRemoveContainer(workload.id)}
						/>
					{/each}
				</tbody>
			</table>
		</div>

		<!-- Pagination bar -->
		{#if totalPages > 1 || filteredWorkloads.length > PAGE_SIZE_OPTIONS[0]}
			<div class="pagination">
				<div class="pagination-info">
					<span class="pg-label">
						{((currentPage - 1) * pageSize) + 1}–{Math.min(currentPage * pageSize, filteredWorkloads.length)} of {filteredWorkloads.length}
					</span>
				</div>

				<div class="pagination-controls">
					<!-- Prev -->
					<button
						class="pg-btn pg-btn--nav"
						type="button"
						onclick={() => goTo(currentPage - 1)}
						disabled={currentPage === 1}
						aria-label="Previous page"
					>
						<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
					</button>

					<!-- Page numbers -->
					{#each pageNumbers as p}
						{#if p === '…'}
							<span class="pg-ellipsis">…</span>
						{:else}
							<button
								class="pg-btn {currentPage === p ? 'pg-btn--active' : ''}"
								type="button"
								onclick={() => goTo(p as number)}
								aria-label="Page {p}"
								aria-current={currentPage === p ? 'page' : undefined}
							>{p}</button>
						{/if}
					{/each}

					<!-- Next -->
					<button
						class="pg-btn pg-btn--nav"
						type="button"
						onclick={() => goTo(currentPage + 1)}
						disabled={currentPage === totalPages}
						aria-label="Next page"
					>
						<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>
					</button>
				</div>

				<div class="pagination-size">
					<span class="pg-label">Rows</span>
					<select class="pg-size-select" bind:value={pageSize}>
						{#each PAGE_SIZE_OPTIONS as size}
							<option value={size}>{size}</option>
						{/each}
					</select>
				</div>
			</div>
		{/if}
	{/if}
</div>

<!-- Slide-over backdrop -->
{#if showCreateForm}
	<div class="drawer-backdrop" role="none" onclick={onToggleCreate}></div>
{/if}

<!-- Slide-over drawer -->
<div class="drawer" class:drawer--open={showCreateForm} aria-hidden={!showCreateForm}>
	<div class="drawer-header">
		<div class="drawer-title-row">
			<h2 class="drawer-title">New sandbox</h2>
			<button class="drawer-close" type="button" onclick={onToggleCreate} aria-label="Close">
				<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
				</svg>
			</button>
		</div>
		<!-- Preset chips -->
		<div class="preset-chips">
			{#each presets as preset}
				<button class="preset-chip" type="button" onclick={() => onApplyPreset(preset.name, preset.image)}>
					{preset.label}
				</button>
			{/each}
		</div>
	</div>

	<!-- Drawer tab bar -->
	<div class="drawer-tabs" role="tablist">
		<button
			class="drawer-tab"
			class:drawer-tab--active={activeDrawerTab === "general"}
			role="tab"
			type="button"
			aria-selected={activeDrawerTab === "general"}
			onclick={() => activeDrawerTab = "general"}
		>General</button>
		<button
			class="drawer-tab"
			class:drawer-tab--active={activeDrawerTab === "git"}
			role="tab"
			type="button"
			aria-selected={activeDrawerTab === "git"}
			onclick={() => activeDrawerTab = "git"}
		>Git</button>
		<button
			class="drawer-tab"
			class:drawer-tab--active={activeDrawerTab === "proxy"}
			role="tab"
			type="button"
			aria-selected={activeDrawerTab === "proxy"}
			onclick={() => activeDrawerTab = "proxy"}
		>
			Proxy
			{#if parsedProxyPorts.length > 0}
				<span class="drawer-tab-badge">{parsedProxyPorts.length}</span>
			{/if}
		</button>
	</div>

	<div class="drawer-body">
		<fieldset class="create-fieldset" disabled={createLoading}>
			{#if createProgress}
				<div class="create-progress create-progress--{createProgress.tone}" role="status" aria-live="polite">
					<div class="create-progress-copy">
						<span class="create-progress-label">Creating sandbox</span>
						<strong>{createProgress.phaseLabel}</strong>
						<p>{createProgress.detail}</p>
					</div>
					<span class="create-progress-status">{createProgress.statusLabel}</span>
				</div>
			{/if}

			<!-- General tab -->
			{#if activeDrawerTab === "general"}
				<div class="form-section">
					<label class="field-col">
						<span class="section-label">Sandbox name</span>
						<input
							class="field"
							class:field--error={!!nameError}
							bind:value={createName}
							placeholder="my-workspace"
							required
							onblur={() => {
								if (formTouched && createName.trim().length === 0) {
									nameError = "Sandbox name is required.";
								}
							}}
							oninput={() => {
								if (createName.trim().length > 0) nameError = "";
							}}
						/>
						{#if nameError}
							<span class="field-inline-error">{nameError}</span>
						{/if}
					</label>
				</div>

				<div class="form-section">
					<span class="section-label">Image</span>
					<label class="field-col">
						<div class:field--error={!!imageError} class="combobox-error-wrapper">
							<Combobox
								bind:value={createExistingImage}
								options={localImageOptions}
								placeholder="Search local images..."
								emptyText={localImageOptions.length === 0 ? "No local images available." : "No matches"}
							/>
						</div>
						{#if localImageOptions.length === 0}
							<div class="create-image-empty">
								<span class="field-help">No local images found. Create or pull one from the Images route.</span>
								<a class="btn-ghost btn-xs" href={createImageHref}>Open Images</a>
							</div>
						{/if}
						{#if imageError}
							<span class="field-inline-error">{imageError}</span>
						{/if}
					</label>
				</div>

				<div class="form-section">
					<span class="section-label form-section-title">Runtime options <span class="opt">(optional)</span></span>
					<label class="field-col">
						<span class="section-label">Workdir</span>
						<input class="field" bind:value={createWorkdir} />
							<span class="field-help">Leave empty to use the image <code class="inline-code">WORKDIR</code>. If the image does not define one, the sandbox keeps the container default working directory and skips the workspace volume.</span>
					</label>
					<div class="field-col">
						<span class="section-label">Environment variables</span>
						<EnvEditor bind:value={createEnv} addLabel="Add variable" emptyMessage="No environment variables configured yet." />
						<span class="field-help">Set runtime variables as key/value pairs. Empty values are allowed; rows without a key are ignored.</span>
					</div>
					<div class="field-col">
						<span class="section-label">Secret environment variables</span>
						<EnvEditor
							bind:value={createSecretEnv}
							addLabel="Add secret"
							emptyMessage="No secret environment variables configured yet."
							valuePlaceholder="Enter a secret value"
							valueInputType="password"
						/>
						<span class="field-help">Secret values are write-only and will not be shown again after save. Rows without both a key and value are ignored.</span>
						{#if createSecretEnvHint}
							<span class="field-help">Secrets are not duplicated; re-enter them manually.</span>
						{/if}
					</div>
					<div class="field-col">
						<span class="section-label">Port mappings <span class="opt">(host → container)</span></span>
						<PortsEditor bind:value={createPorts} />
					</div>
				</div>
			{/if}

			{#if activeDrawerTab === "git"}
				<div class="form-section">
					<span class="section-label form-section-title">Git options <span class="opt">(optional)</span></span>
					<div class="form-row-2">
						<label class="field-col">
							<span class="section-label">Repo URL</span>
							<input
								class="field"
								class:field--error={!!repoUrlError}
								bind:value={createRepoUrl}
								placeholder="https://github.com/org/repo.git"
								oninput={() => {
									const v = createRepoUrl.trim();
									if (v.length === 0 || isValidUrl(v)) repoUrlError = "";
								}}
							/>
							{#if repoUrlError}
								<span class="field-inline-error">{repoUrlError}</span>
							{/if}
						</label>
						<label class="field-col">
							<span class="section-label">Branch</span>
							<input class="field" bind:value={createBranch} placeholder="main" />
						</label>
					</div>
					<div class="form-row-2">
						<label class="field-col">
							<span class="section-label">Base commit</span>
							<input class="field" bind:value={createBaseCommit} placeholder="abc1234" />
							<span class="field-help">Optional commit SHA to clone from.</span>
						</label>
						<label class="field-col">
							<span class="section-label">Clone depth</span>
							<input
								class="field"
								class:field--error={!!depthError}
								bind:value={createDepth}
								type="number"
								min="1"
								step="1"
								inputmode="numeric"
								placeholder="1"
								oninput={() => {
									if (isValidCloneDepth(createDepth)) depthError = "";
								}}
							/>
							<span class="field-help">Optional shallow clone depth. Blank keeps the current behavior.</span>
							{#if depthError}
								<span class="field-inline-error">{depthError}</span>
							{/if}
						</label>
					</div>
					<label class="field-col">
						<span class="section-label">Clone filter</span>
						<input class="field" bind:value={createFilter} placeholder="blob:none" />
						<span class="field-help">Optional partial clone filter passed through when cloning a repo.</span>
					</label>
					<div class="field-col">
						<span class="section-label">Clone behavior</span>
						<Checkbox bind:checked={createSingleBranch} label="Single branch only" />
						<span class="field-help">Limit cloning to the selected branch when supported. Leave off to keep the current multi-branch behavior.</span>
					</div>
				</div>
			{/if}

			<!-- Proxy tab -->
			{#if activeDrawerTab === "proxy"}
				<div class="form-section proxy-tab-section">
					{#if parsedProxyPorts.length === 0}
						<div class="proxy-empty-state">
							<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>
							<p class="field-help">No port mappings defined. Add port mappings on the <button type="button" class="tab-link" onclick={() => activeDrawerTab = "general"}>General</button> tab to configure per-port proxy settings.</p>
						</div>
					{:else}
						{#each parsedProxyPorts as port}
							<div class="proxy-port-block">
								<div class="proxy-port-label">
									<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>
									Port <code class="inline-code">{port}</code>
								</div>
								<ProxyConfigEditor
									value={createProxyConfig[port] ?? null}
									onchange={(v: SandboxPortProxyConfig | null) => {
										if (v === null) {
											const next = { ...createProxyConfig };
											delete next[port];
											createProxyConfig = next;
										} else {
											createProxyConfig = { ...createProxyConfig, [port]: v };
										}
									}}
								/>
							</div>
						{/each}
					{/if}
				</div>
			{/if}

		</fieldset>
	</div>

	<div class="drawer-footer">
		<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate} disabled={createLoading}>
			Cancel
		</button>
		<button class="btn-primary" type="button" onclick={handleSubmit} disabled={createLoading}>
			{#if createLoading}
				<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{createProgress ? `Creating · ${createProgress.phaseLabel}` : "Creating..."}
			{:else}
				Create sandbox
			{/if}
		</button>
	</div>
</div>

<style>
	/* Proxy port block */
	.proxy-port-block {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}

	.proxy-port-label {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
	}

	/* Drawer tab bar */
	.drawer-tabs {
		display: flex;
		gap: 0;
		border-bottom: 1px solid var(--border-dim);
		padding: 0 1.25rem;
		flex-shrink: 0;
	}

	.drawer-tab {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		background: transparent;
		border: none;
		border-bottom: 2px solid transparent;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		padding: 0.55rem 0.75rem 0.45rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s;
		margin-bottom: -1px;
		letter-spacing: 0.03em;
	}

	.drawer-tab:hover {
		color: var(--text-secondary);
	}

	.drawer-tab--active {
		color: var(--text-primary);
		border-bottom-color: var(--accent, var(--text-primary));
	}

	.drawer-tab-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 14px;
		height: 14px;
		padding: 0 3px;
		background: var(--accent-dim, var(--bg-overlay));
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		font-family: var(--font-mono);
		font-size: 0.55rem;
		color: var(--text-secondary);
		line-height: 1;
	}

	/* Proxy tab empty state */
	.proxy-tab-section {
		border-bottom: none;
	}

	.proxy-empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.75rem;
		padding: 2.5rem 1rem;
		text-align: center;
		color: var(--text-muted);
	}

	.proxy-empty-state svg {
		opacity: 0.35;
	}

	.proxy-empty-state .field-help {
		max-width: 260px;
		text-align: center;
	}

	.tab-link {
		background: none;
		border: none;
		padding: 0;
		font-family: inherit;
		font-size: inherit;
		color: var(--text-secondary);
		text-decoration: underline;
		text-underline-offset: 2px;
		cursor: pointer;
		transition: color 0.12s;
	}

	.tab-link:hover {
		color: var(--text-primary);
	}

	/* Inline validation */
	.field--error {
		border-color: var(--status-error-border) !important;
	}
	.field-inline-error {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--status-error);
		margin-top: 0.1rem;
	}
	.combobox-error-wrapper {
		display: contents;
	}
	.combobox-error-wrapper.field--error :global(.combobox-input) {
		border-color: var(--status-error-border) !important;
	}

	.list-view {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		padding: 1.5rem;
		min-height: 100%;
	}

	/* Header */
	.list-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 1rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.list-title-group {
		display: flex;
		align-items: center;
		gap: 0.625rem;
	}

	.list-title {
		font-family: var(--font-display);
		font-size: 1.35rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		margin: 0;
		letter-spacing: -0.01em;
	}

	.list-count {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		padding: 0.1rem 0.45rem;
	}

	.list-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.search-field {
		display: inline-flex;
		align-items: center;
		gap: 0.45rem;
		padding: 0.38rem 0.625rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		color: var(--text-muted);
		min-width: min(22rem, 40vw);
		transition: border-color 0.15s, box-shadow 0.15s;
	}

	.search-field:focus-within {
		border-color: var(--border-focus);
		box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.04);
	}

	.search-icon {
		flex-shrink: 0;
		transition: color 0.15s;
	}

	.search-field:focus-within .search-icon {
		color: var(--text-secondary);
	}

	.search-input {
		border: 0;
		outline: 0;
		background: transparent;
		color: var(--text-primary);
		font-family: var(--font-mono);
		font-size: 0.72rem;
		width: 100%;
	}

	.search-input::placeholder {
		color: var(--text-muted);
	}

	.search-clear {
		display: grid;
		place-items: center;
		width: 18px;
		height: 18px;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		color: var(--text-muted);
		cursor: pointer;
		padding: 0;
		flex-shrink: 0;
		transition: color 0.1s, background 0.1s, border-color 0.1s;
	}

	.search-clear:hover {
		color: var(--text-primary);
		background: var(--accent-dim);
		border-color: var(--border-hi);
	}

	/* Empty state */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.625rem;
		padding: 5rem 2rem;
		text-align: center;
	}

	.empty-icon {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		color: var(--text-muted);
		margin-bottom: 0.5rem;
	}

	.empty-title {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.82rem;
		color: var(--text-secondary);
	}

	.empty-sub {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
	}

	.filter-group {
		display: inline-flex;
		align-items: stretch;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		overflow: hidden;
	}

	.filter-field {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.38rem 0.625rem;
	}

	.filter-sep {
		width: 1px;
		background: var(--border-mid);
		align-self: stretch;
		flex-shrink: 0;
	}

	.filter-label {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		color: var(--text-muted);
		text-transform: uppercase;
		letter-spacing: 0.07em;
		white-space: nowrap;
		flex-shrink: 0;
	}

	.filter-select {
		border: 0;
		outline: 0;
		background: transparent;
		color: var(--text-primary);
		font-family: var(--font-mono);
		font-size: 0.7rem;
		min-width: 6.5rem;
		cursor: pointer;
	}

	.filter-select:disabled {
		color: var(--text-muted);
	}

	/* ── Table ────────────────────────────────────────────────────────────────── */
	.sandbox-table-wrap {
		/* No overflow:hidden — would clip the actions dropdown.
		   Rounded border is achieved via border-separate on the table itself. */
		position: relative;
	}

	.sandbox-table {
		width: 100%;
		/* separate + spacing:0 lets border-radius work without a clipping wrapper */
		border-collapse: separate;
		border-spacing: 0;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
		font-family: var(--font-mono);
		font-size: 0.7rem;
	}

	/* Header */
	.thead-row {
		background: var(--bg-base);
	}

	.th {
		padding: 0.5rem 0.875rem;
		text-align: left;
		font-size: 0.58rem;
		font-weight: 500;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
		white-space: nowrap;
		border-bottom: 1px solid var(--border-dim);
	}

	/* Round the header corners to match the table border-radius */
	.th:first-child { border-top-left-radius: var(--radius-lg); }
	.th:last-child  { border-top-right-radius: var(--radius-lg); }

	/* min-width hints on header cells — auto layout respects these */
	.th:nth-child(1) { min-width: 11rem; } /* Name */
	.th:nth-child(2) { min-width: 10rem; } /* Image */
	.th:nth-child(3) { min-width: 5.5rem; } /* Status */
	.th:nth-child(4) { min-width: 5rem;  } /* Ports */
	.th:nth-child(5) { min-width: 8rem;  } /* Created */
	.th:nth-child(6) { min-width: 6.5rem;} /* Container ID */
	.th:nth-child(7) { min-width: 9rem; } /* Actions — Open + Stop/Start + ⋯ */

	.th-actions {
		text-align: right;
	}

	/* Body rows */
	.sandbox-table tbody :global(tr) {
		transition: background 0.12s;
	}

	.sandbox-table tbody :global(tr:hover) {
		background: var(--bg-raised) !important;
	}

	/* Remove bottom border on last row's cells, round its outer corners */
	.sandbox-table tbody :global(tr:last-child td) {
		border-bottom: none;
	}
	.sandbox-table tbody :global(tr:last-child td:first-child) {
		border-bottom-left-radius: var(--radius-lg);
	}
	.sandbox-table tbody :global(tr:last-child td:last-child) {
		border-bottom-right-radius: var(--radius-lg);
	}

	/* Backdrop */
	.drawer-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0,0,0,0.55);
		z-index: 40;
		animation: fadeIn 0.2s var(--ease-out) both;
	}

	@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

	/* Slide-over drawer */
	.drawer {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(520px, 100vw);
		background: var(--bg-surface);
		border-left: 1px solid var(--border-mid);
		z-index: 50;
		display: flex;
		flex-direction: column;
		transform: translateX(100%);
		transition: transform 0.28s var(--ease-snappy);
		box-shadow: -12px 0 40px rgba(0,0,0,0.45);
	}

	.drawer--open {
		transform: translateX(0);
	}

	.drawer-header {
		padding: 1.125rem 1.25rem 0.875rem;
		border-bottom: 1px solid var(--border-dim);
		flex-shrink: 0;
	}

	.drawer-title-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.75rem;
	}

	.drawer-title {
		margin: 0;
		font-family: var(--font-display);
		font-size: 1.2rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.drawer-close {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.drawer-close:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	.preset-chips {
		display: flex;
		gap: 0.3rem;
		flex-wrap: wrap;
	}

	.preset-chip {
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.2rem 0.55rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.preset-chip:hover {
		color: var(--text-primary);
		border-color: var(--border-mid);
		background: var(--accent-dim);
	}

	/* Drawer body (scrollable) */
	.drawer-body {
		flex: 1;
		overflow-y: auto;
		padding: 1.125rem 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 0;
	}

	.create-fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}

	.create-progress {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.9rem;
		padding: 0.9rem 1rem;
		margin-bottom: 0.85rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-md);
		background: color-mix(in srgb, var(--bg-raised) 88%, transparent);
	}

	.create-progress--active {
		border-color: color-mix(in srgb, var(--accent) 38%, var(--border-mid));
		box-shadow: inset 0 1px 0 rgba(255,255,255,0.04);
	}

	.create-progress--ok {
		border-color: var(--status-ok-border);
	}

	.create-progress--error {
		border-color: var(--status-error-border);
	}

	.create-progress-copy {
		display: flex;
		flex-direction: column;
		gap: 0.22rem;
		min-width: 0;
	}

	.create-progress-copy strong {
		font-size: 0.84rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.create-progress-copy p {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.64rem;
		line-height: 1.5;
		color: var(--text-muted);
	}

	.create-progress-label,
	.create-progress-status {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--text-secondary);
	}

	.create-progress-status {
		flex: none;
		padding-top: 0.05rem;
		text-align: right;
	}

	/* Form sections */
	.form-section {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
		padding: 1rem 0;
		border-bottom: 1px solid var(--border-dim);
	}

	.form-section:last-child {
		border-bottom: none;
	}

	.form-section-title {
		font-size: 0.7rem;
		margin-bottom: 0.25rem;
	}

	.form-row-2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.625rem;
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.opt {
		font-size: 0.58rem;
		color: var(--text-muted);
	}

	.field-help {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		line-height: 1.45;
	}

	.create-image-empty {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.6rem;
		flex-wrap: wrap;
		padding: 0.45rem 0.5rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		background: var(--bg-raised);
	}

	.inline-code {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.05rem 0.3rem;
		color: var(--text-secondary);
	}


	/* Drawer footer */
	.drawer-footer {
		padding: 0.875rem 1.25rem;
		border-top: 1px solid var(--border-dim);
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 0.625rem;
		flex-shrink: 0;
		background: var(--bg-surface);
	}

	/* ── Pagination ─────────────────────────────────────────────────────────── */
	.pagination {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.625rem 0.25rem 0.125rem;
		flex-wrap: wrap;
	}

	.pagination-info {
		flex: 0 0 auto;
	}

	.pagination-controls {
		display: flex;
		align-items: center;
		gap: 0.2rem;
		flex-wrap: nowrap;
	}

	.pagination-size {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		flex: 0 0 auto;
	}

	.pg-label {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		white-space: nowrap;
	}

	.pg-btn {
		display: inline-grid;
		place-items: center;
		min-width: 26px;
		height: 26px;
		padding: 0 0.35rem;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		cursor: pointer;
		transition: color 0.1s, background 0.1s, border-color 0.1s;
		line-height: 1;
	}

	.pg-btn:hover:not(:disabled) {
		color: var(--text-primary);
		background: var(--accent-dim);
		border-color: var(--border-mid);
	}

	.pg-btn:disabled {
		opacity: 0.25;
		cursor: not-allowed;
	}

	.pg-btn--active {
		color: var(--text-primary);
		background: var(--bg-overlay);
		border-color: var(--border-hi);
		cursor: default;
	}

	.pg-btn--active:hover {
		background: var(--bg-overlay);
		border-color: var(--border-hi);
		color: var(--text-primary);
	}

	.pg-btn--nav {
		color: var(--text-secondary);
	}

	.pg-ellipsis {
		display: inline-grid;
		place-items: center;
		min-width: 26px;
		height: 26px;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		letter-spacing: 0.05em;
		user-select: none;
	}

	.pg-size-select {
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		background: var(--bg-raised);
		color: var(--text-primary);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.2rem 0.45rem;
		outline: none;
		cursor: pointer;
		transition: border-color 0.1s;
	}

	.pg-size-select:focus {
		border-color: var(--border-focus);
	}

	/* ── Skeleton loading rows ────────────────────────────────────────────────── */
	@keyframes shimmer {
		0%   { background-position: 200% 0; }
		100% { background-position: -200% 0; }
	}

	.skeleton-row {
		background: transparent;
	}

	.td-skel {
		padding: 0.75rem 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.td-skel--actions {
		text-align: right;
	}

	.skel-line {
		height: 9px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-line--name  { width: 72%; max-width: 9rem; }
	.skel-line--sub   { width: 48%; max-width: 6rem; height: 7px; margin-top: 5px; opacity: 0.6; }
	.skel-line--image { width: 65%; max-width: 8rem; }
	.skel-line--status { width: 4rem; }
	.skel-line--port  { width: 3rem; }
	.skel-line--date  { width: 7rem; }
	.skel-line--id    { width: 5rem; }

	.skel-status {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}

	.skel-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex-shrink: 0;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 0.4rem;
	}

	.skel-btn {
		height: 24px;
		width: 52px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-btn--wide { width: 72px; }

	/* Shared */
	.btn-ghost { display: inline-flex; align-items: center; gap: 0.35rem; }
	.btn-primary { display: inline-flex; align-items: center; gap: 0.4rem; }
	.spin { animation: rotate 0.8s linear infinite; }
	@keyframes rotate { to { transform: rotate(360deg); } }

	@media (max-width: 640px) {
		.list-view { padding: 1rem; }
		.list-header { align-items: stretch; flex-direction: column; }
		.list-actions { width: 100%; flex-wrap: wrap; }
		.search-field { min-width: 100%; }
		.filter-group { width: 100%; }
		.filter-field { flex: 1 1 10rem; }
		.sandbox-table-wrap { overflow-x: auto; }
		.drawer { width: 100vw; }
		.create-progress { flex-direction: column; }
		.create-progress-status { text-align: left; }
		.form-row-2 { grid-template-columns: 1fr; }
		.pagination { flex-direction: column; align-items: flex-start; gap: 0.5rem; }
	}
</style>
