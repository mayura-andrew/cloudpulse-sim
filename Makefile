# Makefile for common tasks

.PHONY: build-server run-server build-frontend run-frontend

build-server:
	cd server && go build -o cloudpulse-server .

run-server:
	cd server && go run . serve

build-frontend:
	cd frontend && npm ci && npm run build

run-frontend:
	cd frontend && npm ci && npm run dev
