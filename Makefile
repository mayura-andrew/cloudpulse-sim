# Makefile for common tasks

.PHONY: build-server run-server build-frontend run-frontend

build-server:
	cd server && go build -o cloudpulse-server ./cmd/server

run-server:
	cd server && go run ./cmd/server serve

build-frontend:
	cd frontend && pnpm install && pnpm build

run-frontend:
	cd frontend && pnpm install && pnpm dev
