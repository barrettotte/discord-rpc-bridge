build:
	go build -o bin/discord-rpc-bridge main.go

run:	build
	go run main.go

clean:
	rm -f bin/discord-rpc-bridge
	rm -f data/*

all:	build
