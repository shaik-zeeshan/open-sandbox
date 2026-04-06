import {
	listContainers,
	listComposeProjects,
	listImages,
	listSandboxes,
	listUsers,
	runApiEffect,
	type ApiConfig,
	type ComposeProjectPreview,
	type ContainerSummary,
	type ImageSummary,
	type Sandbox,
	type UserSummary
} from "$lib/api";
import { Cache, Duration, Effect } from "effect";

type CacheKey = string;

const normalizeBaseUrl = (value: string): string => value.trim().replace(/\/+$/, "");

const cacheKey = (config: ApiConfig): CacheKey => normalizeBaseUrl(config.baseUrl);

const createListCache = <A>(lookup: (baseUrl: string) => Promise<A>): Cache.Cache<CacheKey, A, unknown> =>
	Effect.runSync(
		Cache.make<CacheKey, A, unknown>({
			capacity: 16,
			timeToLive: Duration.infinity,
			lookup: (baseUrl) => Effect.promise(() => lookup(baseUrl))
		})
	);

const readCache = <A>(cache: Cache.Cache<CacheKey, A, unknown>, key: CacheKey): Effect.Effect<A, unknown> =>
	cache.get(key).pipe(Effect.tapError(() => cache.invalidate(key)));

const refreshCache = <A>(cache: Cache.Cache<CacheKey, A, unknown>, key: CacheKey): Effect.Effect<A, unknown> =>
	cache.refresh(key).pipe(Effect.andThen(readCache(cache, key)));

const sandboxesCache = createListCache((baseUrl) => runApiEffect(listSandboxes({ baseUrl })));
const containersCache = createListCache((baseUrl) => runApiEffect(listContainers({ baseUrl })));
const composeProjectsCache = createListCache((baseUrl) => runApiEffect(listComposeProjects({ baseUrl })));
const imagesCache = createListCache((baseUrl) => runApiEffect(listImages({ baseUrl })));
const usersCache = createListCache((baseUrl) => runApiEffect(listUsers({ baseUrl })));

export const getCachedSandboxes = (config: ApiConfig): Effect.Effect<Sandbox[], unknown> =>
	readCache(sandboxesCache, cacheKey(config));

export const refreshCachedSandboxes = (config: ApiConfig): Effect.Effect<Sandbox[], unknown> =>
	refreshCache(sandboxesCache, cacheKey(config));

export const invalidateSandboxesCache = (config: ApiConfig): Effect.Effect<void> =>
	sandboxesCache.invalidate(cacheKey(config));

export const getCachedContainers = (config: ApiConfig): Effect.Effect<ContainerSummary[], unknown> =>
	readCache(containersCache, cacheKey(config));

export const refreshCachedContainers = (config: ApiConfig): Effect.Effect<ContainerSummary[], unknown> =>
	refreshCache(containersCache, cacheKey(config));

export const invalidateContainersCache = (config: ApiConfig): Effect.Effect<void> =>
	containersCache.invalidate(cacheKey(config));

export const getCachedComposeProjects = (config: ApiConfig): Effect.Effect<ComposeProjectPreview[], unknown> =>
	readCache(composeProjectsCache, cacheKey(config));

export const refreshCachedComposeProjects = (config: ApiConfig): Effect.Effect<ComposeProjectPreview[], unknown> =>
	refreshCache(composeProjectsCache, cacheKey(config));

export const invalidateComposeProjectsCache = (config: ApiConfig): Effect.Effect<void> =>
	composeProjectsCache.invalidate(cacheKey(config));

export const getCachedImages = (config: ApiConfig): Effect.Effect<ImageSummary[], unknown> =>
	readCache(imagesCache, cacheKey(config));

export const refreshCachedImages = (config: ApiConfig): Effect.Effect<ImageSummary[], unknown> =>
	refreshCache(imagesCache, cacheKey(config));

export const invalidateImagesCache = (config: ApiConfig): Effect.Effect<void> =>
	imagesCache.invalidate(cacheKey(config));

export const getCachedUsers = (config: ApiConfig): Effect.Effect<UserSummary[], unknown> =>
	readCache(usersCache, cacheKey(config));

export const refreshCachedUsers = (config: ApiConfig): Effect.Effect<UserSummary[], unknown> =>
	refreshCache(usersCache, cacheKey(config));

export const invalidateUsersCache = (config: ApiConfig): Effect.Effect<void> =>
	usersCache.invalidate(cacheKey(config));

export const invalidateWorkloadCaches = (config: ApiConfig): Effect.Effect<void> =>
	Effect.all([
		invalidateSandboxesCache(config),
		invalidateContainersCache(config),
		invalidateComposeProjectsCache(config)
	], { concurrency: "unbounded" }).pipe(
		Effect.asVoid
	);

export const invalidateAllApiCaches = (): Effect.Effect<void> =>
	Effect.all(
		[
			sandboxesCache.invalidateAll,
			containersCache.invalidateAll,
			composeProjectsCache.invalidateAll,
			imagesCache.invalidateAll,
			usersCache.invalidateAll
		],
		{ concurrency: "unbounded" }
	).pipe(Effect.asVoid);

export const invalidateAllApiCachesSync = (): void => {
	Effect.runSync(invalidateAllApiCaches());
};
