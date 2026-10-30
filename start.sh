cd projects/Deep/
git fetch
git pull
go build ./...
set -a && source .env && set +a
go run ./server
