.PHONY: test vet lint build keys up down seed e2e

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

build:
	@mkdir -p bin
	@for d in cmd/*; do \
		s=$$(basename $$d); \
		echo "==> build $$s"; \
		go build -o bin/$$s ./$$d || exit 1; \
	done

keys:
	@mkdir -p keys
	openssl genrsa -out keys/private.pem 2048
	openssl rsa -in keys/private.pem -pubout -out keys/public.pem

COMPOSE := docker compose -f deploy/docker-compose.yml

# Waits for healthchecks, so migrations have run when this returns.
up:
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down -v

# Local/test only. Never run against production.
seed:
	$(COMPOSE) exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d users' < deploy/bootstrap/seed-test-account.sql

e2e:
	bash deploy/e2e_test.sh
