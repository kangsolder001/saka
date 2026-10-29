# Multi-stage Dockerfile for Saka Web SPA

# Stage 1: Base with dependencies
FROM node:22-alpine AS base
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm prepare

# Stage 2: Development server with HMR
FROM base AS dev
EXPOSE 3000
ENV HOST=0.0.0.0
ENV PORT=3000
CMD ["pnpm", "dev", "--host", "0.0.0.0", "--port", "3000"]

# Stage 3: Build static SPA
FROM base AS builder
RUN pnpm build

# Stage 4: Production / Runner with Nginx
FROM nginx:alpine AS runner
COPY --from=builder /app/build /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
HEALTHCHECK --interval=5s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1:3000/ > /dev/null || exit 1
CMD ["nginx", "-g", "daemon off;"]
