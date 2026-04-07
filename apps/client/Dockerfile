FROM oven/bun:1.3.5-alpine AS build

WORKDIR /app

COPY package.json bun.lock ./
COPY apps/client/package.json apps/client/package.json
COPY apps/server/package.json apps/server/package.json
RUN bun install --frozen-lockfile

COPY apps/client ./apps/client

ARG VITE_SANDBOX_BASE_URL=/
ENV VITE_SANDBOX_BASE_URL=${VITE_SANDBOX_BASE_URL}

WORKDIR /app/apps/client
RUN bun run build

FROM joseluisq/static-web-server:2-alpine

COPY apps/client/config.toml /etc/sws/config.toml
COPY --from=build /app/apps/client/build /var/public

EXPOSE 80

CMD ["static-web-server", "--config-file", "/etc/sws/config.toml"]
