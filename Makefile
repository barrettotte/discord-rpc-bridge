APP_NAME = discord-rpc-bridge

build:	clean
	go build -ldflags "-X main.version=dev" -o bin/$(APP_NAME) main.go

run:	build
	./bin/$(APP_NAME)

test:
	go test ./...

lint:
	gofmt -l .
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f bin/$(APP_NAME)

install:
	bash scripts/install.sh

uninstall:
	bash scripts/uninstall.sh

systemd_status:
	systemctl --user status $(APP_NAME)

journal:
	journalctl --user -u $(APP_NAME) -f

all:	build
