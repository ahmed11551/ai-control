.PHONY: env up light full dev
env:
	cp -n .env.example .env || true

up: env
	docker compose up -d --build

light: env
	docker compose up -d --build postgres redis minio minio-init backend frontend nginx

full: env
	docker compose --profile full --profile telephony up -d --build

dev:
	npm run dev
