APP_NAME = discord-rpc-bridge

build:	clean
	go build -o bin/$(APP_NAME) main.go

run:	build
	go run main.go

clean:
	rm -f bin/$(APP_NAME)
	rm -f data/games.json

install:
	bash scripts/install.sh

uninstall:
	bash scripts/uninstall.sh

systemd_status:
	systemctl --user status $(APP_NAME)

journal:
	journalctl --user -u $(APP_NAME) -f

all:	build
