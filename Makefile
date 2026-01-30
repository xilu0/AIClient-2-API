update-js:
	docker build -t heishui/aiclient-2-api:latest .
	cd docker && docker compose up -d aiclient-api


update-go:
	docker build -t heishui/aiclient-go-kiro:latest -f Dockerfile-go .
	cd docker && docker compose up -d go-kiro

update: update-js update-go


build:
	go build -o kiro-server  cmd/kiro-server/main.go

# ssh acc-kiro
# cd ~/src/AIClient-2-API/
# source ~/.relay-claude-cc
# claude

# start chrome and open chrome://inspect/#devices or edge://inspect/#devices
# ssh -R 9222:acc-kiro:9222 acc-kiro
# cd /root/src/AIClient-2-API/.clinic && /root/.nvm/versions/node/v24.13.0/bin/npx -y serve -l 8080

start-js-debug:
	npm run start:debug      # --inspect，调试器可随时连接
#   	npm run start:debug-brk  # --inspect-brk，等待调试器连接

start-js:
	NODE_OPTIONS='--inspect' \
	REDIS_ENABLED=true \
	REDIS_URL=redis://127.0.0.1:6379 \
	REDIS_KEY_PREFIX=aiclient: \
	node src/core/master.js --api-key AI_club2026

goreplay:
	gor --input-raw eth0:8080 --output-file docs/goreplay/requests.gor