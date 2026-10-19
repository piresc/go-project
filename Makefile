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

# Local/test only. Never run against production. The seed bypasses user.created,
# so it also clears any portfolio of the seeded user (id from
# seed-test-account.sql) in the asset database; holdings cascade.
seed:
	$(COMPOSE) exec -T postgres-user sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d users' < deploy/bootstrap/seed-test-account.sql
	echo "DELETE FROM portfolios WHERE user_id = '11111111-1111-1111-1111-111111111111';" | $(COMPOSE) exec -T postgres-asset sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d assets'

# The e2e script also stops/starts containers and inspects Redis (E2E_DOCKER=1).
e2e:
	E2E_DOCKER=1 bash deploy/e2e_test.sh
