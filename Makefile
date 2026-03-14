reg:
	docker build -t registry.mix.local/mixer/picstore .
	docker push registry.mix.local/mixer/picstore

up-store:
	docker compose up pic-database pic-redis

run:
	go run ./cmd/main/main.go

down:
	docker compose down