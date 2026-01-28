update:
	docker build -t heishui/aiclient-2-api:latest .
	cd docker && docker compose up -d --build


build:
	go build -o kiro-server  cmd/kiro-server/main.go