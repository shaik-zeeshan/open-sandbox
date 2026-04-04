.PHONY: server client dev test-server check-client build-client

server:
	bun run dev:server

client:
	bun run dev:client

dev:
	bun run dev

test-server:
	bun run test:server

check-client:
	bun run check:client

build-client:
	bun run build:client
