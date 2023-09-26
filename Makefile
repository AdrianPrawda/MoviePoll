.PHONY: clean db run-api run-render

api:
	mkdir -p build/
	go build -C src/server/api -o ../../../build/server/api

render:
	mkdir -p build/
	go build -C src/server/render -o ../../../build/server/render

build: api render

db:
	mkdir -p db/
	sqlite3 db/poll.db '.read src/server/api/init.sql'

clean:
	rm -rf build/
	rm -f db/poll.db

run-api: build-api db
	./build/api

run-render: build-render db
	./build/render
