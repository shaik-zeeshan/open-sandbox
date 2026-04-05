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

FROM nginx:1.27-alpine

COPY apps/client/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/apps/client/build /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
