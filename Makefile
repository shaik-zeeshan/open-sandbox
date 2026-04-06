SHELL := /bin/sh

COMPOSE_FILE ?= compose.yaml
COMPOSE_DEV_FILE ?= compose.dev.yaml
COMPOSE_GHCR_FILE ?= compose.ghcr.yaml

.PHONY: \
	help \
	install \
	dev server client \
	build build-server build-client \
	check check-client \
	test test-server \
	up down logs ps config \
	compose-build compose-up compose-down compose-logs compose-ps compose-config \
	compose-dev-up compose-dev-down compose-dev-logs compose-dev-ps compose-dev-config \
	compose-ghcr-pull compose-ghcr-up compose-ghcr-down compose-ghcr-logs compose-ghcr-ps compose-ghcr-config

help:
	@printf "Open Sandbox commands\n\n"
	@printf "Workspace\n"
	@printf "  make install            Install Bun workspace dependencies\n"
	@printf "  make dev                Run full monorepo dev (Turbo)\n"
	@printf "  make server             Run server dev loop (air)\n"
	@printf "  make client             Run client dev loop (vite)\n"
	@printf "  make check              Run all configured checks\n"
	@printf "  make check-client       Run Svelte checks\n"
	@printf "  make test               Run all configured tests\n"
	@printf "  make test-server        Run Go backend tests\n"
	@printf "  make build              Run all builds\n"
	@printf "  make build-server       Build Go server binary\n"
	@printf "  make build-client       Build client static assets\n\n"
	@printf "Compose (build from source via compose.yaml)\n"
	@printf "  make up                 Alias for compose-up\n"
	@printf "  make down               Alias for compose-down\n"
	@printf "  make logs               Alias for compose-logs\n"
	@printf "  make ps                 Alias for compose-ps\n"
	@printf "  make config             Alias for compose-config\n"
	@printf "  make compose-build      docker compose build\n"
	@printf "  make compose-up         docker compose up -d\n"
	@printf "  make compose-down       docker compose down\n"
	@printf "  make compose-logs       docker compose logs -f\n"
	@printf "  make compose-ps         docker compose ps\n"
	@printf "  make compose-config     docker compose config\n\n"
	@printf "Compose dev hot reload (compose.dev.yaml)\n"
	@printf "  make compose-dev-up     Run DX stack in foreground\n"
	@printf "  make compose-dev-down   Stop DX stack\n"
	@printf "  make compose-dev-logs   Follow DX stack logs\n"
	@printf "  make compose-dev-ps     Show DX stack status\n"
	@printf "  make compose-dev-config Validate DX compose config\n\n"
	@printf "Compose GHCR images (compose.ghcr.yaml)\n"
	@printf "  make compose-ghcr-pull  Pull image tags\n"
	@printf "  make compose-ghcr-up    Start GHCR stack\n"
	@printf "  make compose-ghcr-down  Stop GHCR stack\n"
	@printf "  make compose-ghcr-logs  Follow GHCR logs\n"
	@printf "  make compose-ghcr-ps    Show GHCR status\n"
	@printf "  make compose-ghcr-config Validate GHCR compose config\n"

install:
	bun install --frozen-lockfile

dev:
	bun run dev

server:
	bun run dev:server

client:
	bun run dev:client

build:
	bun run build

build-server:
	bun run build:server

build-client:
	bun run build:client

check:
	bun run check

check-client:
	bun run check:client

test:
	bun run test

test-server:
	bun run test:server

compose-build:
	docker compose -f $(COMPOSE_FILE) build

compose-up:
	docker compose -f $(COMPOSE_FILE) up -d

compose-down:
	docker compose -f $(COMPOSE_FILE) down

compose-logs:
	docker compose -f $(COMPOSE_FILE) logs -f

compose-ps:
	docker compose -f $(COMPOSE_FILE) ps

compose-config:
	docker compose -f $(COMPOSE_FILE) config

up: compose-up

down: compose-down

logs: compose-logs

ps: compose-ps

config: compose-config

compose-dev-up:
	docker compose -f $(COMPOSE_DEV_FILE) up

compose-dev-down:
	docker compose -f $(COMPOSE_DEV_FILE) down

compose-dev-logs:
	docker compose -f $(COMPOSE_DEV_FILE) logs -f

compose-dev-ps:
	docker compose -f $(COMPOSE_DEV_FILE) ps

compose-dev-config:
	docker compose -f $(COMPOSE_DEV_FILE) config

compose-ghcr-pull:
	docker compose -f $(COMPOSE_GHCR_FILE) pull

compose-ghcr-up:
	docker compose -f $(COMPOSE_GHCR_FILE) up -d

compose-ghcr-down:
	docker compose -f $(COMPOSE_GHCR_FILE) down

compose-ghcr-logs:
	docker compose -f $(COMPOSE_GHCR_FILE) logs -f

compose-ghcr-ps:
	docker compose -f $(COMPOSE_GHCR_FILE) ps

compose-ghcr-config:
	docker compose -f $(COMPOSE_GHCR_FILE) config
