SERVICES := asset-service price-service user-service

.PHONY: test vet lint build keys up down seed e2e

test:
	@for s in $(SERVICES); do \
		echo "==> test $$s"; \
		(cd services/$$s && go test -race -count=1 ./...) || exit 1; \
	done

vet:
	@for s in $(SERVICES); do \
		echo "==> vet $$s"; \
		(cd services/$$s && go vet ./...) || exit 1; \
	done

lint:
	@for s in $(SERVICES); do \
		echo "==> lint $$s"; \
		(cd services/$$s && golangci-lint run) || exit 1; \
	done

build:
	@mkdir -p bin
	@for s in $(SERVICES); do \
		echo "==> build $$s"; \
		(cd services/$$s && go build -o ../../bin/$$s ./cmd/server) || exit 1; \
	done

keys:
	@mkdir -p keys
	openssl genrsa -out keys/private.pem 2048
	openssl rsa -in keys/private.pem -pubout -out keys/public.pem

# Placeholders: filled in by the compose/e2e task.
up:
	@echo "not implemented yet"

down:
	@echo "not implemented yet"

seed:
	@echo "not implemented yet"

e2e:
	@echo "not implemented yet"
