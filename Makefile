.PHONY: server client dev test-server check-client build-client

server:
	go run ./apps/server

client:
	bun --cwd apps/client dev

dev:
	@trap 'kill 0' EXIT; $(MAKE) server & $(MAKE) client

test-server:
	go test ./apps/server/...

check-client:
	bun --cwd apps/client check

build-client:
	bun --cwd apps/client build
