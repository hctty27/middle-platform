# syntax=docker/dockerfile:1
FROM node:22-alpine AS build
WORKDIR /src/config-center-admin
COPY config-center-admin/package*.json ./
RUN npm ci --no-audit --no-fund
COPY config-center-admin/ ./
RUN npm run build

FROM nginx:stable-alpine
COPY deploy/nginx/admin-static.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/config-center-admin/dist /usr/share/nginx/html
EXPOSE 80
