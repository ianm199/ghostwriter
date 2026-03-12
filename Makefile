APP_NAME := Ghostwriter
BINARY   := ghostwriter
BUILD_DIR := build
APP_BUNDLE := $(BUILD_DIR)/$(APP_NAME).app

.PHONY: build app clean install

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/ghostwriter/

BUNDLE_ID := com.ghostwriter.app

app: build
	mkdir -p $(APP_BUNDLE)/Contents/MacOS
	cp $(BUILD_DIR)/$(BINARY) $(APP_BUNDLE)/Contents/MacOS/$(BINARY)
	cp packaging/Info.plist $(APP_BUNDLE)/Contents/Info.plist
	codesign --force --sign - --identifier $(BUNDLE_ID) $(APP_BUNDLE)

clean:
	rm -rf $(BUILD_DIR)

install: app
	$(APP_BUNDLE)/Contents/MacOS/$(BINARY) install --app-bundle $(APP_BUNDLE)
