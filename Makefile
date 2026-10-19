.PHONY: test vet lint build keys up down db-init seed e2e

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

# Re-runs the idempotent init script (roles and schemas) in the running
# postgres container. A fresh volume runs it automatically; use this to add a
# new service's role and schema to an existing volume, after editing init-db.sh.
# The script is baked into the image, so rebuild first (`make up`) after edits.
db-init:
	$(COMPOSE) exec -T postgres bash /docker-entrypoint-initdb.d/00-init-db.sh

# Local/test only. Never run against production. Runs as the admin role with an
# explicit search_path, so the SQL stays unqualified. The seed bypasses
# user.created, so it also clears any portfolio of the seeded user (id from
# seed-test-account.sql) in the assets schema; holdings cascade.
seed:
	$(COMPOSE) exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "SET search_path = users" -f -' < deploy/bootstrap/seed-test-account.sql
	echo "DELETE FROM portfolios WHERE user_id = '11111111-1111-1111-1111-111111111111';" | $(COMPOSE) exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "SET search_path = assets" -f -'

# The e2e script also stops/starts containers and inspects Redis (E2E_DOCKER=1).
e2e:
	E2E_DOCKER=1 bash deploy/e2e_test.sh
