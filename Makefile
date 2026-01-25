update:
	docker build -t heishui/aiclient-2-api:latest .
	cd docker && docker compose up -d