SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.PHONY: init deps up down run build format test verify integration security openapi

init:
	python3 scripts/dev.py init

deps: init
	docker compose up -d --wait postgres redis s3 mailpit

up: init
	docker compose --profile app up -d --build --wait

down:
	docker compose --profile app down

run: build
	python3 scripts/dev.py run

build:
	mkdir -p bin
	go build -trimpath -o bin/dicho ./cmd/server
	go build -trimpath -o bin/dicho-keygen ./cmd/keygen
	go build -trimpath -o bin/dicho-migrate ./cmd/migrate

format:
	@files="$$(gofmt -l cmd internal api migrations)"; if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

test:
	python3 -m unittest discover -s scripts -p 'test_*.py'
	go test ./... -count=1 -race

verify: format
	go vet ./...
	go mod verify
	$(MAKE) test build

integration:
	mkdir -p ../output/backend-qa
	go test -tags integration -coverpkg=./... ./... -count=1 -race -coverprofile=../output/backend-qa/coverage.out -json -timeout 5m > ../output/backend-qa/integration.jsonl
	@python3 -c 'import json,collections; c=collections.Counter(x["Action"] for line in open("../output/backend-qa/integration.jsonl") if "Test" in (x:=json.loads(line)) and x["Action"] in ("pass","fail","skip")); print("Test cases:",dict(c))'
	python3 scripts/coverage_report.py ../output/backend-qa/coverage.out --tests ../output/backend-qa/integration.jsonl

security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

openapi:
	python3 api/build.py
	git diff --exit-code -- api/openapi.json

.PHONY: smoke
smoke:
	python3 scripts/smoke.py

.PHONY: secrets
secrets:
	mkdir -p ../output/backend-qa
	go run github.com/zricethezav/gitleaks/v8@v8.30.1 git --redact --no-banner --report-format json --report-path ../output/backend-qa/gitleaks-reviewed.json ..

.PHONY: recovery
recovery:
	python3 scripts/recovery_check.py

.PHONY: migrate
migrate:
	go run ./cmd/migrate

.PHONY: performance
performance:
	go test -tags performance ./internal/app -run '^TestPerformance' -count=1 -v -timeout 10m
