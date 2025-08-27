NAKAMA_SERVER_PATH ?= ../server

build:
	docker run --rm -w "/builder" -v "${PWD}:/builder" heroiclabs/nakama-pluginbuilder:3.27.1 build -buildmode=plugin -trimpath -o ./build/backend.so

copy:
	cp ./build/backend.so "${NAKAMA_SERVER_PATH}/modules/backend.so"

PHONY: build copy # Added copy to PHONY targets